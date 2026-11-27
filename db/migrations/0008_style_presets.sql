-- Style presets: the deployment's recipes for a look (docs/tuning.md#style-presets).
-- Keyed per lineage because recipes don't transfer between model lineages.
-- Prompt fields merge into every request that names the style; settings fill
-- only what the request left unset. Resolved values are stored on the job
-- row, so editing a preset never changes history. Adding a style is an
-- INSERT in a new migration, not a deploy.
CREATE TABLE style_presets (
    slug          text NOT NULL,
    lineage       text NOT NULL,                 -- illustrious | noobai | ...
    name          text NOT NULL,
    description   text NOT NULL DEFAULT '',
    family        text NOT NULL,                 -- picker group: mihoyo | anime | illustration
    lora          text,                          -- filename under LORA_DIR; NULL = tags only
    lora_strength double precision,
    prompt_prefix text NOT NULL DEFAULT '',      -- style carriers, lead the prompt
    prompt_suffix text NOT NULL DEFAULT '',      -- finish scaffold, trail the prompt
    negative      text NOT NULL DEFAULT '',      -- appended after the user's negative
    sampler       text,
    scheduler     text,
    cfg           double precision,
    steps         int,
    cfg_rescale   double precision,
    hires         boolean,                       -- recommended size tier for the client; core never forces it
    hires_scale   double precision,
    hires_denoise double precision,
    sort_order    int     NOT NULL DEFAULT 0,
    enabled       boolean NOT NULL DEFAULT TRUE,
    PRIMARY KEY (slug, lineage)
);

-- The one measured recipe: eval/benchmarks/mihoyo-style.md round 6 keeper
-- (HSR v3 LoRA @ 0.8, grounded finish scaffold, cfg_rescale 0.45, hires
-- 1.5x/0.55). (logo:1.3) is mandatory — the v3 LoRA renders the HSR logo
-- on txt2img. v-pred only: the recipe was tuned on NoobAI.
INSERT INTO style_presets (slug, lineage, name, description, family, lora, lora_strength,
    prompt_prefix, prompt_suffix, negative, sampler, scheduler, cfg, steps, cfg_rescale,
    hires, hires_scale, hires_denoise, sort_order)
VALUES (
    'hsr', 'noobai', 'Honkai: Star Rail',
    'miHoYo house style through the HSR v3 LoRA: official-art finish, smooth gradients, soft unbroken linework. Generalises across IPs — Genshin, ZZZ and HI3 characters render through it.',
    'mihoyo', 'honkai star rail_v3.safetensors', 0.8,
    'official art, game cg',
    'painterly, gradient hair, gradient eyes, sparkling eyes, eyelashes, shiny skin, glint, sparkle, glowing, depth of field, blurry background',
    '(logo:1.3), film grain, scan artifacts, halftone, chromatic aberration, realistic, photorealistic',
    'euler', 'normal', 5.0, 28, 0.45,
    TRUE, 1.5, 0.55, 10
);

-- Tag-only looks, both lineages. No measured settings: they take the
-- lineage defaults. Carrier tags are Danbooru vocabulary the models learned
-- (ground any addition with GET /tags/lookup first — CLAUDE.md #4).
INSERT INTO style_presets (slug, lineage, name, description, family, prompt_prefix, prompt_suffix, negative, sort_order)
SELECT v.slug, l.lineage, v.name, v.description, v.family, v.prompt_prefix, v.prompt_suffix, v.negative, v.sort_order
FROM (VALUES
    ('game-cg', 'Game CG',
     'Official-art / game-CG finish from tags alone. The stand-in for Genshin, ZZZ and HI3 until their style LoRAs land; pairs with a character LoRA.',
     'mihoyo', 'official art, game cg',
     'painterly, gradient hair, gradient eyes, sparkling eyes, shiny skin, glint, sparkle, depth of field, blurry background',
     'film grain, scan artifacts, halftone, chromatic aberration, realistic, photorealistic', 20),
    ('light-novel', 'Light novel',
     'Light-novel cover illustration: clean colour, soft gradients, shallow focus.',
     'illustration', 'novel illustration, official art',
     'gradient hair, sparkling eyes, depth of field, blurry background',
     'realistic, photorealistic, 3d', 30),
    ('anime-90s', '90s anime',
     'Retro cel look: flat colour, hard shadow, the 1990s TV palette.',
     'anime', '1990s (style), retro artstyle',
     'anime coloring',
     'realistic, photorealistic, 3d', 40),
    ('flat-color', 'Flat colour',
     'Modern cel shading: flat fills, crisp linework, minimal gradients.',
     'anime', 'flat color, anime coloring',
     '',
     'realistic, photorealistic, 3d', 50),
    ('watercolor', 'Watercolour',
     'Traditional-media wash: soft edges, paper texture, muted palette.',
     'illustration', 'watercolor (medium), traditional media',
     'painterly',
     'realistic, photorealistic, 3d', 60),
    ('sketch', 'Sketch',
     'Monochrome pencil / lineart study.',
     'illustration', 'sketch, lineart, monochrome, greyscale',
     '',
     'realistic, photorealistic', 70),
    ('chibi', 'Chibi',
     'Super-deformed proportions, big head, simplified features.',
     'anime', 'chibi',
     '',
     'realistic, photorealistic', 80)
) AS v (slug, name, description, family, prompt_prefix, prompt_suffix, negative, sort_order)
CROSS JOIN (VALUES ('noobai'), ('illustrious')) AS l (lineage);
