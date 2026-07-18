// Package api is the HTTP surface (docs/architecture.md#endpoints).
package api

import (
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
	"regexp"
	"strconv"
	"strings"
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
	IPAdapterFrag  *workflow.Fragment
	IPAdapter2Frag *workflow.Fragment
	Regional2Frag  *workflow.Fragment
	Regional3Frag  *workflow.Fragment
	Cfg            Config
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /generate", s.handleGenerate)
	mux.HandleFunc("GET /jobs/{id}", s.handleJob)
	mux.HandleFunc("GET /jobs/{id}/stream", s.handleStream)
	mux.HandleFunc("GET /images/{id}", s.handleImage)
	mux.HandleFunc("POST /uploads", s.handleUpload)
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

	spec, err := assembly.Build(req.Request, assembly.Options{
		Lineage: s.Cfg.Lineage, Seed: seed,
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

	var graph json.RawMessage
	switch req.Kind {
	case "pose":
		graph, err = workflow.BuildPose(s.PoseTpl, spec, s.Cfg.Checkpoint)
	case "img2img":
		graph, err = workflow.BuildImg2Img(s.Img2ImgTpl, spec, s.Cfg.Checkpoint, req.ImageID+".png")
	case "inpaint":
		graph, err = workflow.BuildInpaint(s.InpaintTpl, spec, s.Cfg.Checkpoint, req.ImageID+".png", req.MaskID+".png")
	default:
		graph, err = workflow.BuildTxt2Img(s.Tpl, spec, s.Cfg.Checkpoint)
	}
	if err == nil && req.RefID != "" {
		graph, err = workflow.ApplyIPAdapter(graph, s.IPAdapterFrag, req.RefID+".png", spec.RefStrength)
	}
	// regional characters: generate column masks (the shared masking
	// subsystem), store them as aux blobs, compose the regional fragment
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
			if err == nil && charRefs == 2 {
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
