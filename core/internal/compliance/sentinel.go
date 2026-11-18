// Sentinel is the phase-4 checker: a self-hosted WD-tagger sidecar scores
// images; policy is applied here (docs/compliance.md).
package compliance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"pulsar/core/internal/assembly"
	"pulsar/core/internal/tags"
)

// Threshold bounds: env can tune sensitivity but never disable the gate.
// Two rules block an output: any watch tag over the max gate, or the SUM of
// watch scores over the sum gate — age tags are weakly represented in the
// tagger (Danbooru purges that content), so several faint signals together
// are the reliable discriminator (docs/compliance.md#current-implementation).
const (
	minThreshold     = 0.05
	maxThreshold     = 0.60
	defaultThreshold = 0.35
	minSumThreshold  = 0.02
	maxSumThreshold  = 0.20
	defaultSum       = 0.05
)

type Sentinel struct {
	url  string
	http *http.Client
	// watch are normalized forbidden tags (from FORBIDDEN_TAG_UC): the
	// output gate's scoring set, unchanged by the prompt-weighting work.
	watch []string
	// include is the classifier's include= list: tags.WatchSet (floor ∪
	// FORBIDDEN_TAG_UC), so every watched tag's score is always present.
	include   string
	threshold float64
	sumGate   float64
	prompts   *Prompts
}

// NewSentinel: complementTags is AGE_COMPLEMENT_TAGS (may be empty).
func NewSentinel(url, forbiddenUC, complementTags string, threshold, sumGate float64) *Sentinel {
	if threshold == 0 {
		threshold = defaultThreshold
	}
	if sumGate == 0 {
		sumGate = defaultSum
	}
	threshold = min(max(threshold, minThreshold), maxThreshold)
	sumGate = min(max(sumGate, minSumThreshold), maxSumThreshold)
	watch := tags.ParseForbidden(forbiddenUC)
	include := make([]string, 0)
	for t := range tags.WatchSet(forbiddenUC) {
		include = append(include, t)
	}
	sort.Strings(include)
	return &Sentinel{
		url:       strings.TrimRight(url, "/"),
		http:      &http.Client{Timeout: 30 * time.Second},
		watch:     watch,
		include:   strings.Join(include, ","),
		threshold: threshold,
		sumGate:   sumGate,
		prompts:   NewPrompts(forbiddenUC, complementTags),
	}
}

// CheckPrompt applies the prompt policy (prompt.go) to every surface
// ComfyUI receives — positive, negative and each character prompt.
func (s *Sentinel) CheckPrompt(_ context.Context, spec assembly.Spec) (Decision, error) {
	return s.prompts.Check(spec)
}

func (s *Sentinel) CheckOutput(ctx context.Context, image []byte) (Decision, error) {
	return s.classify(ctx, image)
}

func (s *Sentinel) CheckUpload(ctx context.Context, image []byte) (Decision, error) {
	return s.classify(ctx, image)
}

type scores struct {
	Rating map[string]float64 `json:"rating"`
	Tags   map[string]float64 `json:"tags"`
}

func (s *Sentinel) classify(ctx context.Context, image []byte) (Decision, error) {
	// include= forces watch tags into the response even sub-floor
	u := s.url + "/classify?include=" + s.include
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(image))
	if err != nil {
		return Decision{}, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := s.http.Do(req)
	if err != nil {
		return Decision{}, fmt.Errorf("sentinel: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return Decision{}, fmt.Errorf("sentinel: %s: %.200s", resp.Status, raw)
	}
	var sc scores
	if err := json.Unmarshal(raw, &sc); err != nil {
		return Decision{}, fmt.Errorf("sentinel: bad response: %w", err)
	}

	explicit := sc.Rating["explicit"] + sc.Rating["questionable"]
	gate, sumGate := s.threshold, s.sumGate
	if explicit >= 0.5 {
		gate, sumGate = gate/2, sumGate/2
	}
	sum := 0.0
	for _, w := range s.watch {
		score := sc.Tags[w]
		sum += score
		if score >= gate {
			return Decision{
				Allowed: false,
				Reason:  fmt.Sprintf("%s=%.2f (gate %.2f, explicit %.2f)", w, score, gate, explicit),
			}, nil
		}
	}
	if sum >= sumGate {
		return Decision{
			Allowed: false,
			Reason:  fmt.Sprintf("watch sum=%.3f (gate %.3f, explicit %.2f)", sum, sumGate, explicit),
		}, nil
	}
	return Decision{Allowed: true}, nil
}
