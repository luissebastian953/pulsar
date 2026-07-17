package workflow

// Node IDs renumber on re-export — update constants + hash together
// (.claude/skills/comfy-workflow).
const (
	TplTxt2Img    = "txt2img.json"
	Txt2ImgSHA256 = "sha256:d95928e83803e01d802cfdaa86c8756d6a533513009a8cd82b9b07db22aa0320"

	TplPose    = "pose.json"
	PoseSHA256 = "sha256:f34d3ba787c5ba399e4be95329adec279d62cc4fe70d8a89e36bfbb263c5c722"
)

var Txt2Img = struct {
	Checkpoint string
	Positive   string
	Negative   string
	Latent     string
	Sampler    string
	Decode     string
	Save       string
}{
	Checkpoint: "1",
	Positive:   "2",
	Negative:   "3",
	Latent:     "4",
	Sampler:    "5",
	Decode:     "6",
	Save:       "7",
}

// Pose = txt2img spine (1–7) + deterministic extraction
// (.claude/skills/comfy-workflow).
var Pose = struct {
	Checkpoint    string
	Positive      string
	Negative      string
	Latent        string
	Sampler       string
	Decode        string
	SaveSource    string
	Lineart       string
	SaveLineart   string
	Openpose      string
	SaveSkeleton  string
	LineartInvert string
}{
	Checkpoint:   "1",
	Positive:     "2",
	Negative:     "3",
	Latent:       "4",
	Sampler:      "5",
	Decode:       "6",
	SaveSource:   "7",
	Lineart:      "8",
	SaveLineart:  "9",
	Openpose:     "10",
	SaveSkeleton: "11",
	// flips white-on-black conditioning to black-on-white for delivery
	LineartInvert: "12",
}

// OutputRoles: kind → role → SaveImage node; role "" is the primary
// (.claude/skills/core-job).
func OutputRoles(kind string) (map[string]string, bool) {
	switch kind {
	case "txt2img":
		return map[string]string{"": Txt2Img.Save}, true
	case "pose":
		return map[string]string{
			"":         Pose.SaveSource,
			"lineart":  Pose.SaveLineart,
			"skeleton": Pose.SaveSkeleton,
		}, true
	}
	return nil, false
}
