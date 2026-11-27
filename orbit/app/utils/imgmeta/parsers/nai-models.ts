// NovelAI `Source` → model. The trailing 8-hex hash decides; the prefix does
// not (early V4.5 images say `DiffusionModelMetaName.NAIv4next 4BDE2A90`).
// Hashes and ids are the ones NovelAI's frontend maps (function `tb`, bundle
// 0duset3_dg_wg.js, Nov 2026); display names are pulsar's wording.

const MODELS: Record<string, string> = {
  'nai-diffusion-5-full': 'NAI Diffusion V5 Full',
  'nai-diffusion-5-curated': 'NAI Diffusion V5 Curated',
  'nai-diffusion-5-full-medium': 'NAI Diffusion V5 Full (Medium)',
  'nai-diffusion-4-5-full': 'NAI Diffusion V4.5 Full',
  'nai-diffusion-4-5-curated': 'NAI Diffusion V4.5 Curated',
  'nai-diffusion-4-full': 'NAI Diffusion V4 Full',
  'nai-diffusion-4-curated-preview': 'NAI Diffusion V4 Curated',
  'nai-diffusion-3': 'NAI Diffusion Anime V3',
  'nai-diffusion-furry-3': 'NAI Diffusion Furry V3',
  'nai-diffusion-2': 'NAI Diffusion Anime V2',
  'nai-diffusion': 'NAI Diffusion Anime V1 (Full)',
  'safe-diffusion': 'NAI Diffusion Anime V1 (Curated)',
  'nai-diffusion-furry': 'NAI Diffusion Furry',
}

const BY_HASH: Record<string, string> = {}

function add(id: string, hashes: string) {
  for (const h of hashes.split(' ')) BY_HASH[h] = id
}

add('nai-diffusion-5-full', '657484A5 0ADF9AB7')
add('nai-diffusion-5-curated', 'DB276663')
add('nai-diffusion-5-full-medium', '93F4BD30 70AB5786')
add('nai-diffusion-4-5-full', '4BDE2A90 1229B44F B9F340FD F3D95188')
add('nai-diffusion-4-5-curated', 'C02D4F98 5AB81C7C B5A2A797')
add('nai-diffusion-4-full', '37442FCA 4F49EC75 CA4B7203 79F47848 F6302A9D')
add('nai-diffusion-4-curated-preview', '7ABFFA2A C1CCBA86 770A9E12')
add('safe-diffusion', '1D44365E F4D50568')
add('nai-diffusion', '81274D13 3B3287AF')
add('nai-diffusion-furry', '4CC42576 1D09C008 1D09D794 F64BA557')
add('nai-diffusion-2', '49BFAF6A F1022D28')
add('nai-diffusion-3', 'B0BDF6C1 C1E1DE52 7BCCAA2C 1120E6A9 8BA2AF87')
add('nai-diffusion-furry-3', '4BE8C60C C8704949 37C2B166 F306816B 9CC2F394')

export interface NaiModel {
  /** NovelAI's model id, or `unknown`. */
  id: string
  /** Display name, or `Unknown`. */
  name: string
  hash?: string
}

/** Resolve a `Source` string such as `NovelAI Diffusion V5 DB276663`. */
export function lookupNaiModel(source: string | undefined): NaiModel {
  const text = source ?? ''
  const hash = text.match(/(?:^|\s)([0-9A-Fa-f]{8})\s*$/)?.[1]?.toUpperCase()
  let id = hash ? BY_HASH[hash] : undefined

  // Unknown hash: NovelAI falls back on the version in the name.
  if (!id) {
    if (text.includes('V5')) id = 'nai-diffusion-5-curated'
    else if (text.includes('V4.5')) id = 'nai-diffusion-4-5-curated'
    else if (text.includes('V4')) id = 'nai-diffusion-4-curated-preview'
  }

  const model: NaiModel = { id: id ?? 'unknown', name: (id && MODELS[id]) || 'Unknown' }

  if (hash) model.hash = hash

  return model
}
