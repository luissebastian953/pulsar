package workflow

// Node IDs renumber on re-export — update constants + hash together
// (.claude/skills/comfy-workflow).
const (
	TplTxt2Img    = "txt2img.json"
	Txt2ImgSHA256 = "sha256:d95928e83803e01d802cfdaa86c8756d6a533513009a8cd82b9b07db22aa0320"

	TplPose    = "pose.json"
	PoseSHA256 = "sha256:f34d3ba787c5ba399e4be95329adec279d62cc4fe70d8a89e36bfbb263c5c722"

	TplImg2Img    = "img2img.json"
	Img2ImgSHA256 = "sha256:b8dfd8604a5b21bbc6e126ee1fc7be74444be7c670028b3a0ba5cb11e138ee41"

	// fragments live under patches/ and compose onto any base sharing the
	// spine IDs (.claude/skills/comfy-workflow)
	FragIPAdapter   = "patches/ipadapter.json"
	IPAdapterSHA256 = "sha256:dbd6bba89c2810e8e487c3037252b5273bdec22226ecfdc3c15cd795c101c99b"

	TplInpaint    = "inpaint.json"
	InpaintSHA256 = "sha256:e1323578c44df103a93f5f9fdde55143fa8498ada345584007399a499d93c3d7"
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

// Img2Img = txt2img spine minus the empty latent: LoadImage → VAEEncode
// feeds the sampler, denoise < 1 keeps composition (.claude/skills/comfy-workflow).
var Img2Img = struct {
	Checkpoint string
	Positive   string
	Negative   string
	Sampler    string
	Decode     string
	Save       string
	LoadImage  string
	Encode     string
}{
	Checkpoint: "1",
	Positive:   "2",
	Negative:   "3",
	Sampler:    "5",
	Decode:     "6",
	Save:       "7",
	LoadImage:  "8",
	Encode:     "9",
}

// Inpaint = img2img + a mask: white repaints, black keeps
// (.claude/skills/comfy-workflow).
var Inpaint = struct {
	Checkpoint string
	Positive   string
	Negative   string
	Sampler    string
	Save       string
	LoadImage  string
	LoadMask   string
	NoiseMask  string
}{
	Checkpoint: "1",
	Positive:   "2",
	Negative:   "3",
	Sampler:    "5",
	Save:       "7",
	LoadImage:  "8",
	LoadMask:   "10",
	NoiseMask:  "12",
}

// IPAdapter fragment node IDs (reserved range 20+ never collides with a
// base template).
var IPAdapter = struct {
	LoadRef string
	Loader  string
	Apply   string
}{
	LoadRef: "20",
	Loader:  "21",
	Apply:   "22",
}

// OutputRoles: kind → role → SaveImage node; role "" is the primary
// (.claude/skills/core-job).
func OutputRoles(kind string) (map[string]string, bool) {
	switch kind {
	case "txt2img":
		return map[string]string{"": Txt2Img.Save}, true
	case "img2img":
		return map[string]string{"": Img2Img.Save}, true
	case "inpaint":
		return map[string]string{"": Inpaint.Save}, true
	case "pose":
		return map[string]string{
			"":         Pose.SaveSource,
			"lineart":  Pose.SaveLineart,
			"skeleton": Pose.SaveSkeleton,
		}, true
	}
	return nil, false
}
