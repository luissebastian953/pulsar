package tags

// poseSeed seeds pose_relevant; the DB is ground truth
// (docs/tagdb.md#pose-subset). Entries resolve through aliases at ingest, so
// renamed tags still land; seeds with no tag row are reported (PoseMissing),
// never stored. Keep this the closed vocabulary a *pose sketch* needs — body
// position, camera, framing, held objects — not general art vocabulary.
var poseSeed = []string{
	// body
	"standing", "sitting", "kneeling", "squatting", "crouching", "lying",
	"on_back", "on_stomach", "on_side", "all_fours", "jumping", "running",
	"walking", "leaning_forward", "bent_over", "arched_back", "crossed_legs",
	"arms_up", "arms_behind_head", "arms_behind_back", "crossed_arms",
	"hand_on_hip", "outstretched_arm", "reaching", "looking_back",
	"head_tilt", "twisted_torso", "contrapposto", "dynamic_pose",
	// head & gaze
	"looking_at_viewer", "looking_up", "looking_down", "looking_to_the_side",
	"looking_away", "looking_ahead", "head_down", "head_back",
	// arms & hands
	"arm_up", "arm_behind_back", "arm_behind_head", "arms_at_sides",
	"outstretched_arms", "spread_arms", "outstretched_hand", "hand_up",
	"hands_up", "hand_on_own_chest", "hand_on_own_face", "hand_on_own_head",
	"hand_in_pocket", "hands_in_pockets", "clenched_hand", "clenched_hands",
	"open_hand", "open_hands", "spread_fingers", "own_hands_together",
	"own_hands_clasped", "interlocked_fingers", "pointing",
	"pointing_at_viewer", "pointing_up", "index_finger_raised", "thumbs_up",
	"waving", "salute", "reaching_towards_viewer", "stretching", "flexing",
	"carrying",
	// legs & feet
	"standing_on_one_leg", "leg_up", "legs_up", "leg_lift", "knees_up",
	"knees_together_feet_apart", "spread_legs", "crossed_ankles", "kicking",
	"high_kick", "tiptoes", "on_one_knee", "seiza", "wariza", "indian_style",
	"hugging_own_legs", "fetal_position", "lunging", "crawling",
	// torso & airborne
	"leaning_back", "leaning_to_the_side", "upside-down", "midair", "falling",
	"floating", "flying",
	// action
	"fighting_stance", "punching", "blocking", "dancing", "climbing",
	"hanging", "swimming", "throwing",
	// holding — the with/without-object half of the sketch brief
	"holding", "holding_weapon", "holding_sword", "holding_gun",
	"holding_staff", "holding_polearm", "holding_bow_(weapon)",
	"holding_shield", "holding_knife", "holding_axe", "holding_scythe",
	"holding_umbrella", "holding_book", "holding_phone", "holding_cup",
	"holding_food", "holding_instrument", "holding_microphone",
	"holding_ball", "holding_bag", "holding_flower", "holding_sign",
	// weaponry & objects
	"weapon", "sword", "katana", "rapier", "dagger", "knife", "gun",
	"handgun", "rifle", "shotgun", "sniper_rifle", "polearm", "spear",
	"lance", "naginata", "halberd", "scythe", "axe", "battle_axe", "hammer",
	"war_hammer", "staff", "bow_(weapon)", "crossbow", "shield", "whip",
	"umbrella", "book", "cellphone", "smartphone", "broom", "guitar",
	"electric_guitar", "microphone", "baseball_bat", "basketball",
	"soccer_ball", "tennis_racket", "fishing_rod", "frying_pan", "shovel",
	"wrench", "paintbrush",
	// weapon handling
	"dual_wielding", "unsheathing", "sheathed", "aiming", "aiming_at_viewer",
	"weapon_on_back", "weapon_over_shoulder", "sword_over_shoulder",
	"planted",
	// camera
	"from_above", "from_below", "from_side", "from_behind", "dutch_angle",
	"foreshortening", "perspective",
	// framing
	"full_body", "upper_body", "cowboy_shot", "portrait",
	// backdrop — the /pose background toggle grounds through these
	"simple_background", "white_background", "grey_background",
	"transparent_background", "scenery",
}
