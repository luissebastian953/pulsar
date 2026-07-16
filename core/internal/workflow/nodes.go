package workflow

// Node IDs for workflows/txt2img.json. IDs renumber when a template is
// re-exported from the ComfyUI editor — update these constants and the hash
// in the same change (.claude/skills/comfy-workflow/).
const (
	TplTxt2Img    = "txt2img.json"
	Txt2ImgSHA256 = "sha256:d95928e83803e01d802cfdaa86c8756d6a533513009a8cd82b9b07db22aa0320"
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
