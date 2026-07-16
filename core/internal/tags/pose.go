package tags

// poseSeed is the curated seed for pose_relevant, from
// docs/tagdb.md#pose-subset. It is a seed, not ground truth — the DB is
// ground truth: ingest marks matches, reports misses for review, and never
// unmarks tags (hand curation in the DB survives re-ingest).
var poseSeed = []string{
	// body
	"standing", "sitting", "kneeling", "squatting", "crouching", "lying",
	"on_back", "on_stomach", "on_side", "all_fours", "jumping", "running",
	"walking", "leaning_forward", "bent_over", "arched_back", "crossed_legs",
	"arms_up", "arms_behind_head", "arms_behind_back", "crossed_arms",
	"hand_on_hip", "outstretched_arm", "reaching", "looking_back",
	"head_tilt", "twisted_torso", "contrapposto", "dynamic_pose",
	// camera
	"from_above", "from_below", "from_side", "from_behind", "dutch_angle",
	"foreshortening", "perspective",
	// framing
	"full_body", "upper_body", "cowboy_shot", "portrait",
}
