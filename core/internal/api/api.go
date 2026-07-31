// Package api is the HTTP surface (docs/architecture.md#endpoints).
package api

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"pulsar/core/internal/assembly"
	"pulsar/core/internal/blob"
	"pulsar/core/internal/compliance"
	"pulsar/core/internal/ingest"
	"pulsar/core/internal/job"
	"pulsar/core/internal/masks"
	"pulsar/core/internal/nltag"
	"pulsar/core/internal/tags"
	"pulsar/core/internal/workflow"
)

type Config struct {
	Lineage    string
	Checkpoint string
	// prompt config from env; ForbiddenTagUC is required (see assembly.Options)
	QualityTags    string
	DefaultUC      string
	ForbiddenTagUC string
	// CORSOrigins is the allowed cross-origin list; "*" allows any. Empty
	// disables CORS entirely (same-origin only).
	CORSOrigins []string
}

type Server struct {
	Store   *job.Store
	Broker  *job.Broker
	Checker compliance.Checker
	Blobs   blob.Store
	Tags    *tags.Store
	// Live is the Danbooru live validator; nil degrades /tags/lookup to
	// DB-only resolution.
	Live *tags.Live
	// Translator is nil when no LLM is configured; POST /translate then 503s.
	Translator *nltag.Translator
	Tpl        workflow.Graph // txt2img base, hash-verified at load
	PoseTpl    workflow.Graph // pose base (spine + extraction), hash-verified at load
	Img2ImgTpl workflow.Graph // img2img base (LoadImage → VAEEncode), hash-verified at load
	InpaintTpl workflow.Graph // inpaint base (img2img + noise mask), hash-verified at load
	// fragments compose onto any built base (hash-verified)
	IPAdapterFrag   *workflow.Fragment
	IPAdapter2Frag  *workflow.Fragment
	IPAdapterCSFrag *workflow.Fragment
	Regional2Frag   *workflow.Fragment
	Regional3Frag   *workflow.Fragment
	HiresFrag       *workflow.Fragment
	LoraFrag        *workflow.Fragment
	Lora2Frag       *workflow.Fragment
	VpredFrag       *workflow.Fragment
	// LoraDir / CheckpointDir list installed files for GET /loras and
	// GET /checkpoints (empty = none listed).
	LoraDir       string
	CheckpointDir string
	Cfg           Config

	// auto-reference blob cache: post-cutoff characters are rare and their
	// consensus exemplar is stable for a process lifetime
	autoRefMu sync.Mutex
	autoRefs  map[string]string // canonical → blob id
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /generate", s.handleGenerate)
	mux.HandleFunc("GET /jobs/{id}", s.handleJob)
	mux.HandleFunc("GET /jobs/{id}/stream", s.handleStream)
	mux.HandleFunc("GET /images/{id}", s.handleImage)
	mux.HandleFunc("POST /uploads", s.handleUpload)
	mux.HandleFunc("GET /loras", s.handleLoras)
	mux.HandleFunc("GET /checkpoints", s.handleCheckpoints)
	mux.HandleFunc("GET /tags", s.handleTags)
	mux.HandleFunc("GET /tags/lookup", s.handleTagLookup)
	mux.HandleFunc("POST /translate", s.handleTranslate)
	mux.HandleFunc("GET /history", s.handleHistory)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	if len(s.Cfg.CORSOrigins) > 0 {
		return cors(mux, s.Cfg.CORSOrigins)
	}
	return mux
}

var jobIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// job id plus optional role suffix (…-lineart, …-skeleton)
var imageIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}(-[a-z]{1,16})?$`)

// POST /generate: validate → CheckPrompt → assemble → build workflow →
// enqueue → 202. Generation is 20–40s; it never blocks this handler.
func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		assembly.Request
		// Kind selects the workflow: "txt2img" (default), "img2img",
		// "inpaint" (image_id [+ mask_id] from POST /uploads), or "pose".
		Kind    string `json:"kind"`
		ImageID string `json:"image_id"`
		MaskID  string `json:"mask_id"`
		// RefID composes the Precise Reference fragment onto any
		// non-pose kind (docs/precise-reference.md)
		RefID string `json:"reference_id"`
		// Checkpoint overrides the default base model; must be installed
		// (same lineage — settings don't transfer).
		Checkpoint string `json:"checkpoint"`
		// Lineage overrides the deployment default (env LINEAGE) per request
		// so eps and v-pred bases can be A/B'd on one deployment. Pair it
		// with a matching Checkpoint — a v-pred checkpoint on an eps lineage
		// (or vice versa) produces garbage (docs/tuning.md).
		Lineage string `json:"lineage"`
		// AutoReference (default on) escalates a post-cutoff character to a
		// consensus Danbooru reference image when no reference/image was
		// given (docs/tagdb.md#anchors). false = anchors only.
		AutoReference *bool `json:"auto_reference"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Kind == "" {
		req.Kind = "txt2img"
	}
	if _, ok := workflow.OutputRoles(req.Kind); !ok {
		httpError(w, http.StatusBadRequest, "unknown kind "+strconv.Quote(req.Kind))
		return
	}
	charRefs := 0
	for _, c := range req.Characters {
		if c.ReferenceID != "" {
			charRefs++
		}
	}
	if charRefs > 0 {
		switch {
		case req.RefID != "":
			httpError(w, http.StatusBadRequest, "use either a global reference or per-character references, not both")
			return
		case len(req.Characters) != 2 || charRefs != 2:
			httpError(w, http.StatusBadRequest, "per-character references require exactly 2 characters, each with a reference")
			return
		}
		for i, c := range req.Characters {
			if !imageIDPattern.MatchString(c.ReferenceID) {
				httpError(w, http.StatusBadRequest, fmt.Sprintf("character %d: invalid reference_id", i+1))
				return
			}
			if _, _, err := s.blobDims(c.ReferenceID); err != nil {
				httpError(w, http.StatusBadRequest, fmt.Sprintf("character %d: unknown reference_id", i+1))
				return
			}
		}
	}
	if req.RefID != "" {
		if req.Kind == "pose" {
			httpError(w, http.StatusBadRequest, "reference does not apply to pose")
			return
		}
		if !imageIDPattern.MatchString(req.RefID) {
			httpError(w, http.StatusBadRequest, "invalid reference_id")
			return
		}
		if _, _, err := s.blobDims(req.RefID); err != nil {
			httpError(w, http.StatusBadRequest, "unknown reference_id")
			return
		}
		if req.RefStrength == nil {
			d := 0.8 // strong identity, still promptable (docs/precise-reference.md)
			req.RefStrength = &d
		}
		if req.RefFidelity == nil {
			f := 0.5 // → end_at 0.7, the live-tuned default
			req.RefFidelity = &f
		}
	}
	if req.Kind == "img2img" || req.Kind == "inpaint" {
		if !imageIDPattern.MatchString(req.ImageID) {
			httpError(w, http.StatusBadRequest, req.Kind+" requires image_id from POST /uploads")
			return
		}
		iw, ih, err := s.blobDims(req.ImageID)
		if err != nil {
			httpError(w, http.StatusBadRequest, "unknown image_id")
			return
		}
		if req.Kind == "inpaint" {
			if !imageIDPattern.MatchString(req.MaskID) {
				httpError(w, http.StatusBadRequest, "inpaint requires mask_id from POST /uploads")
				return
			}
			mw, mh, err := s.blobDims(req.MaskID)
			if err != nil {
				httpError(w, http.StatusBadRequest, "unknown mask_id")
				return
			}
			if mw != iw || mh != ih {
				httpError(w, http.StatusBadRequest,
					fmt.Sprintf("mask %dx%d does not match image %dx%d", mw, mh, iw, ih))
				return
			}
		}
		if req.Denoise == nil {
			d := 0.6 // restructure while keeping composition
			if req.Kind == "inpaint" {
				d = 0.85 // masked-area replacement wants strength
			}
			req.Denoise = &d
		}
	}

	seed := int64(0)
	if req.Seed != nil {
		seed = *req.Seed
	} else {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			httpError(w, http.StatusInternalServerError, "seed generation failed")
			return
		}
		seed = int64(binary.BigEndian.Uint64(b[:]) >> 1) // non-negative
	}

	lineage := s.Cfg.Lineage
	if req.Lineage != "" {
		lineage = req.Lineage // validated in assembly.Build (unknown → 400)
	}
	// post-cutoff characters render by description: inject their Danbooru
	// design vocabulary after the character tag, then escalate to a
	// consensus reference image — text carries the palette, pixels carry
	// the identity (docs/tagdb.md#anchors)
	if s.Live != nil {
		var newer []string
		req.Prompt, newer = s.autoAnchor(r.Context(), req.Prompt, lineage)
		if len(newer) > 0 && req.Kind == "txt2img" && req.RefID == "" && req.ImageID == "" &&
			charRefs == 0 && (req.AutoReference == nil || *req.AutoReference) {
			if id, err := s.autoReference(r.Context(), newer[0]); err != nil {
				log.Printf("api: auto reference %q: %v", newer[0], err)
			} else if id != "" {
				req.RefID = id
				if req.RefStrength == nil {
					d := 0.55 // v-pred char-mode stable band tops out ~0.55 (docs/tuning.md)
					req.RefStrength = &d
				}
				if req.RefFidelity == nil {
					f := 0.5
					req.RefFidelity = &f
				}
				if req.RefMode == "" {
					req.RefMode = "character"
				}
				log.Printf("api: auto reference attached for %q (blob %s)", newer[0], id)
			}
		}
	}
	spec, err := assembly.Build(req.Request, assembly.Options{
		Lineage: lineage, Seed: seed,
		QualityTags: s.Cfg.QualityTags, DefaultUC: s.Cfg.DefaultUC, ForbiddenTagUC: s.Cfg.ForbiddenTagUC,
	})
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	// pre-enqueue compliance hook; an unavailable checker rejects (fail closed)
	decision, err := s.Checker.CheckPrompt(r.Context(), spec)
	if err != nil {
		httpError(w, http.StatusServiceUnavailable, "compliance check unavailable")
		return
	}
	if !decision.Allowed {
		httpError(w, http.StatusUnprocessableEntity, "prompt rejected: "+decision.Reason)
		return
	}

	ckpt := s.Cfg.Checkpoint
	if req.Checkpoint != "" {
		if !s.checkpointInstalled(req.Checkpoint) {
			httpError(w, http.StatusBadRequest, "unknown checkpoint "+strconv.Quote(req.Checkpoint))
			return
		}
		ckpt = req.Checkpoint
	}
	spec.Checkpoint = ckpt

	var graph json.RawMessage
	switch req.Kind {
	case "pose":
		graph, err = workflow.BuildPose(s.PoseTpl, spec, ckpt)
	case "img2img":
		graph, err = workflow.BuildImg2Img(s.Img2ImgTpl, spec, ckpt, req.ImageID+".png")
	case "inpaint":
		graph, err = workflow.BuildInpaint(s.InpaintTpl, spec, ckpt, req.ImageID+".png", req.MaskID+".png")
	default:
		graph, err = workflow.BuildTxt2Img(s.Tpl, spec, ckpt)
	}
	if err == nil && req.RefID != "" {
		// fidelity → step range: how long the reference steers the denoise
		endAt := 0.4 + 0.6*spec.RefFidelity
		switch spec.RefMode {
		case "character_style":
			graph, err = workflow.ApplyIPAdapterCS(graph, s.IPAdapterCSFrag, req.RefID+".png", spec.RefStrength, endAt)
		case "style":
			graph, err = workflow.ApplyIPAdapter(graph, s.IPAdapterFrag, req.RefID+".png", spec.RefStrength, "style transfer", endAt)
		default: // character / identity
			graph, err = workflow.ApplyIPAdapter(graph, s.IPAdapterFrag, req.RefID+".png", spec.RefStrength, "", endAt)
		}
	}
	var auxIDs []string
	if err == nil && len(spec.Characters) > 0 {
		if req.Kind == "pose" {
			httpError(w, http.StatusBadRequest, "characters do not apply to pose")
			return
		}
		frag := s.Regional2Frag
		if len(spec.Characters) == 3 {
			frag = s.Regional3Frag
		}
		colMasks, merr := masks.Columns(len(spec.Characters), spec.Width, spec.Height, 0.06)
		if merr != nil {
			err = merr
		} else {
			maskNames := make([]string, 0, len(colMasks))
			prompts := make([]string, 0, len(spec.Characters))
			for i, m := range colMasks {
				id, ierr := newUploadID()
				if ierr != nil {
					err = ierr
					break
				}
				if perr := s.Blobs.Put(id, m); perr != nil {
					err = perr
					break
				}
				auxIDs = append(auxIDs, id)
				maskNames = append(maskNames, id+".png")
				prompts = append(prompts, spec.Characters[i].Prompt)
			}
			if err == nil {
				graph, err = workflow.ApplyRegional(graph, frag, maskNames, prompts)
			}
			if err == nil && charRefs == 2 { // per-char refs need the regional masks
				var refNames [2]string
				var weights [2]float64
				for i, c := range spec.Characters {
					refNames[i] = c.ReferenceID + ".png"
					weights[i] = 0.8
					if c.RefStrength != nil {
						weights[i] = *c.RefStrength
					}
					auxIDs = append(auxIDs, c.ReferenceID)
				}
				graph, err = workflow.ApplyIPAdapter2(graph, s.IPAdapter2Frag, refNames, weights)
			}
		}
	}
	if err != nil {
		log.Printf("api: build workflow: %v", err)
		httpError(w, http.StatusInternalServerError, "workflow build failed")
		return
	}

	if err == nil && len(spec.Loras) > 0 {
		if req.RefID != "" || len(spec.Characters) > 0 {
			httpError(w, http.StatusBadRequest, "LoRA cannot combine with references or characters yet")
			return
		}
		l := spec.Loras[0]
		graph, err = workflow.ApplyLora(graph, s.LoraFrag, l.Name, *l.Strength)
		if err == nil && len(spec.Loras) == 2 {
			l2 := spec.Loras[1]
			graph, err = workflow.ApplyLora2(graph, s.Lora2Frag, l2.Name, *l2.Strength)
		}
	}
	// v-pred wrap after the model-chain fragments (so it converts the
	// composed model) but before hires (so the wrap carries into the refine
	// pass). eps lineages skip it entirely.
	if err == nil && assembly.Vpred(spec.Lineage) {
		graph, err = workflow.ApplyVpred(graph, s.VpredFrag, spec.CFGRescale)
	}
	// hires last: it mirrors the base sampler's links, so every earlier
	// fragment (reference, regional, lora, v-pred wrap) carries into the refine pass
	if err == nil && spec.Hires {
		if req.Kind == "pose" {
			httpError(w, http.StatusBadRequest, "hires does not apply to pose")
			return
		}
		graph, err = workflow.ApplyHires(graph, s.HiresFrag, spec)
	}
	if err != nil {
		log.Printf("api: compose fragments: %v", err)
		httpError(w, http.StatusInternalServerError, "workflow build failed")
		return
	}

	params, err := json.Marshal(spec)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "params marshal failed")
		return
	}
	j, err := s.Store.Enqueue(r.Context(), req.Kind, params, graph, req.ImageID, req.MaskID, req.RefID, auxIDs)
	if err != nil {
		log.Printf("api: enqueue: %v", err)
		httpError(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": j.ID})
}

type jobPayload struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	State    job.State       `json:"state"`
	Params   json.RawMessage `json:"params"`
	ImageURL string          `json:"image_url,omitempty"`
	// per-role URLs for multi-output kinds; image_url stays the primary
	Images        map[string]string `json:"images,omitempty"`
	Error         string            `json:"error,omitempty"`
	QueuePosition int               `json:"queue_position,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
}

func (s *Server) payload(r *http.Request, j *job.Job) jobPayload {
	p := jobPayload{
		ID: j.ID, Kind: j.Kind, State: j.State,
		Params: j.Params, Error: j.Error, CreatedAt: j.CreatedAt,
	}
	if j.State == job.StateDone && j.ImagePath != "" {
		p.ImageURL = "/images/" + j.ID
		if roles, ok := workflow.OutputRoles(j.Kind); ok && len(roles) > 1 {
			p.Images = make(map[string]string, len(roles))
			for role := range roles {
				if role == "" {
					p.Images["source"] = "/images/" + j.ID
				} else {
					p.Images[role] = "/images/" + j.ID + "-" + role
				}
			}
		}
	}
	if j.State == job.StateQueued {
		if pos, err := s.Store.QueuePosition(r.Context(), j.ID); err == nil {
			p.QueuePosition = pos
		}
	}
	return p
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) (*job.Job, bool) {
	id := r.PathValue("id")
	if !jobIDPattern.MatchString(id) {
		httpError(w, http.StatusNotFound, "no such job")
		return nil, false
	}
	j, err := s.Store.Get(r.Context(), id)
	if errors.Is(err, job.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such job")
		return nil, false
	}
	if err != nil {
		log.Printf("api: get job: %v", err)
		httpError(w, http.StatusInternalServerError, "lookup failed")
		return nil, false
	}
	return j, true
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	j, ok := s.getJob(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.payload(r, j))
}

// POST /uploads — the single image-ingest path (img2img, later references
// and inpaint): normalize → CheckUpload → store (.claude/skills/core-job).
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(ingest.MaxBytes); err != nil {
		httpError(w, http.StatusBadRequest, "multipart form required")
		return
	}
	f, _, err := r.FormFile("image")
	if err != nil {
		httpError(w, http.StatusBadRequest, "image field required")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, ingest.MaxBytes+1))
	if err != nil {
		httpError(w, http.StatusBadRequest, "read failed")
		return
	}

	norm, err := ingest.Normalize(data)
	if err != nil {
		httpError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// ingest compliance hook — fail closed
	dec, err := s.Checker.CheckUpload(r.Context(), norm.PNG)
	if err != nil {
		httpError(w, http.StatusServiceUnavailable, "compliance check unavailable")
		return
	}
	if !dec.Allowed {
		httpError(w, http.StatusUnprocessableEntity, "upload rejected: "+dec.Reason)
		return
	}

	id, err := newUploadID()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "id generation failed")
		return
	}
	if err := s.Blobs.Put(id, norm.PNG); err != nil {
		log.Printf("api: upload store: %v", err)
		httpError(w, http.StatusInternalServerError, "store failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"image_id": id, "width": norm.Width, "height": norm.Height,
	})
}

// listModels returns installed model filenames in dir.
func listModels(dir string) []string {
	out := []string{}
	if dir == "" {
		return out
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if n := e.Name(); !e.IsDir() && (strings.HasSuffix(n, ".safetensors") || strings.HasSuffix(n, ".pt")) {
			out = append(out, n)
		}
	}
	return out
}

// GET /loras — installed LoRA filenames for the picker.
func (s *Server) handleLoras(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"loras": listModels(s.LoraDir)})
}

// checkpointInstalled reports whether name is installed, or equals the
// configured default (so single-checkpoint deployments still validate).
func (s *Server) checkpointInstalled(name string) bool {
	if name == s.Cfg.Checkpoint {
		return true
	}
	for _, c := range listModels(s.CheckpointDir) {
		if c == name {
			return true
		}
	}
	return false
}

// GET /checkpoints — installed base models; active default marked so the
// UI can preselect it.
func (s *Server) handleCheckpoints(w http.ResponseWriter, r *http.Request) {
	names := listModels(s.CheckpointDir)
	if len(names) == 0 && s.Cfg.Checkpoint != "" {
		names = []string{s.Cfg.Checkpoint}
	}
	writeJSON(w, http.StatusOK, map[string]any{"checkpoints": names, "active": s.Cfg.Checkpoint})
}

// blobDims reads just the PNG header of a stored blob.
func (s *Server) blobDims(id string) (int, int, error) {
	rc, err := s.Blobs.Open(id)
	if err != nil {
		return 0, 0, err
	}
	defer rc.Close()
	cfg, _, err := image.DecodeConfig(rc)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

// newUploadID is a v4-style UUID so upload blobs share the job-id namespace
// rules (sanitizer + serving pattern).
func newUploadID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !imageIDPattern.MatchString(id) {
		httpError(w, http.StatusNotFound, "no such image")
		return
	}
	rc, err := s.Blobs.Open(id)
	if err != nil {
		httpError(w, http.StatusNotFound, "no such image")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	io.Copy(w, rc)
}

// GET /tags?q=&subset= — autocomplete over the tag DB; subset=pose grounds
// against the closed pose vocabulary (docs/tagdb.md#ui-contract).
func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	var poseOnly bool
	switch r.URL.Query().Get("subset") {
	case "", "all":
	case "pose":
		poseOnly = true
	default:
		httpError(w, http.StatusBadRequest, "subset must be all or pose")
		return
	}
	suggestions, err := s.Tags.Autocomplete(r.Context(), r.URL.Query().Get("q"), poseOnly, 20)
	if err != nil {
		log.Printf("api: tags: %v", err)
		httpError(w, http.StatusInternalServerError, "autocomplete failed")
		return
	}
	out := make([]tagView, 0, len(suggestions))
	for _, sg := range suggestions {
		out = append(out, tagView{
			Canonical: sg.Canonical, Category: sg.Category, Count: sg.Count,
			NewerThanModel: s.newerThanModel(sg.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": out})
}

// tagView is the wire shape for suggestions and lookups; NewerThanModel
// flags tags the active lineage never saw (docs/tagdb.md).
type tagView struct {
	Canonical      string `json:"canonical"`
	Category       string `json:"category"`
	Count          int    `json:"count"`
	Stage          string `json:"stage,omitempty"`
	NewerThanModel bool   `json:"newer_than_model,omitempty"`
}

// autoAnchor: for each character tag in the prompt that post-dates the
// lineage's knowledge cutoff, splice its Danbooru design vocabulary in
// right after the tag — the model can't know the name but knows every
// component. Missing created_at dates (pre-freshness seeds) backfill
// through Live, which also heals the autocomplete newer_than_model flag.
// Every failure degrades to the unmodified prompt; anchoring never blocks
// a generation. Also returns the post-cutoff character tags it found, so
// the caller can escalate to an auto reference.
func (s *Server) autoAnchor(ctx context.Context, prompt, lineage string) (string, []string) {
	var newer []string
	cutoff, ok := assembly.KnowledgeCutoff(lineage)
	if !ok || strings.TrimSpace(prompt) == "" {
		return prompt, nil
	}
	parts := strings.Split(prompt, ",")
	present := make(map[string]bool, len(parts))
	for _, p := range parts {
		present[tags.Normalize(p)] = true
	}
	var out []string
	for _, p := range parts {
		out = append(out, p)
		canonical := tags.Normalize(p)
		if canonical == "" {
			continue
		}
		res, err := s.Tags.Resolve(ctx, canonical, false)
		if err != nil || res == nil || res.Category != "character" ||
			(res.Stage != "exact" && res.Stage != "alias") {
			continue
		}
		created := res.CreatedAt
		if created == nil {
			if created, err = s.Live.EnsureCreatedAt(ctx, res.Canonical); err != nil {
				log.Printf("api: anchor backfill %q: %v", res.Canonical, err)
				continue
			}
		}
		if created == nil || !created.After(cutoff) {
			continue // the model learned this character; no anchors needed
		}
		newer = append(newer, res.Canonical)
		anchors, err := s.Live.DesignAnchors(ctx, res.Canonical, present)
		if err != nil {
			log.Printf("api: design anchors %q: %v", res.Canonical, err)
			continue
		}
		for _, a := range anchors {
			present[a] = true
			out = append(out, " "+strings.ReplaceAll(a, "_", " "))
		}
		if len(anchors) > 0 {
			log.Printf("api: anchored post-cutoff character %q with %d design tags", res.Canonical, len(anchors))
		}
	}
	return strings.Join(out, ","), newer
}

// autoReference resolves a post-cutoff character to a reference blob: the
// consensus Danbooru exemplar, pulled through the SAME ingest path as user
// uploads — normalize (strips metadata) then the compliance hook, fail
// closed (CLAUDE.md: image ingest is one path). Empty id = no suitable or
// permitted reference; the caller degrades to anchors only.
func (s *Server) autoReference(ctx context.Context, canonical string) (string, error) {
	s.autoRefMu.Lock()
	if id, ok := s.autoRefs[canonical]; ok {
		s.autoRefMu.Unlock()
		if _, _, err := s.blobDims(id); err == nil {
			return id, nil
		} // blob vanished (data dir reset) — refetch below
	} else {
		s.autoRefMu.Unlock()
	}

	data, postID, err := s.Live.ConsensusReference(ctx, canonical, ingest.MaxBytes)
	if err != nil || data == nil {
		return "", err
	}
	// identity lives in the face; a full-body exemplar imports its outfit
	// texture into everything (docs/tagdb.md#anchors)
	if cropped, err := ingest.BustCrop(data); err == nil {
		data = cropped
	}
	norm, err := ingest.Normalize(data)
	if err != nil {
		return "", fmt.Errorf("post %d: %w", postID, err)
	}
	dec, err := s.Checker.CheckUpload(ctx, norm.PNG)
	if err != nil {
		return "", fmt.Errorf("compliance unavailable: %w", err)
	}
	if !dec.Allowed {
		return "", fmt.Errorf("post %d rejected: %s", postID, dec.Reason)
	}
	id, err := newUploadID()
	if err != nil {
		return "", err
	}
	if err := s.Blobs.Put(id, norm.PNG); err != nil {
		return "", err
	}
	s.autoRefMu.Lock()
	if s.autoRefs == nil {
		s.autoRefs = make(map[string]string)
	}
	s.autoRefs[canonical] = id
	s.autoRefMu.Unlock()
	return id, nil
}

func (s *Server) newerThanModel(createdAt *time.Time) bool {
	if createdAt == nil {
		return false
	}
	cutoff, ok := assembly.KnowledgeCutoff(s.Cfg.Lineage)
	return ok && createdAt.After(cutoff)
}

// GET /tags/lookup?name=X — live validator: DB first, Danbooru on miss
// (.claude/skills/tag-grounding).
func (s *Server) handleTagLookup(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if strings.TrimSpace(name) == "" {
		httpError(w, http.StatusBadRequest, "name is required")
		return
	}
	var (
		res *tags.Resolution
		err error
	)
	if s.Live != nil {
		res, err = s.Live.Validate(r.Context(), name)
	} else {
		res, err = s.Tags.Resolve(r.Context(), name, false)
	}
	if err != nil {
		log.Printf("api: tag lookup %q: %v", name, err)
		httpError(w, http.StatusBadGateway, "lookup failed")
		return
	}
	if res == nil {
		writeJSON(w, http.StatusOK, map[string]any{"found": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"found": true,
		"tag": tagView{
			Canonical: res.Canonical, Category: res.Category, Count: res.Count,
			Stage: res.Stage, NewerThanModel: s.newerThanModel(res.CreatedAt),
		},
	})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.Store.History(r.Context(), 50)
	if err != nil {
		log.Printf("api: history: %v", err)
		httpError(w, http.StatusInternalServerError, "history failed")
		return
	}
	out := make([]jobPayload, 0, len(jobs))
	for _, j := range jobs {
		p := jobPayload{ID: j.ID, Kind: j.Kind, State: j.State, Params: j.Params, CreatedAt: j.CreatedAt}
		if j.State == job.StateDone && j.ImagePath != "" {
			p.ImageURL = "/images/" + j.ID
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Ping(r.Context()); err != nil {
		httpError(w, http.StatusServiceUnavailable, "db unreachable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
