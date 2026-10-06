// De veldgeluiden van de module (CC0) en hun herkomst, uit de
// mediacatalogus van de module zelf: module/duif/config/media-manifest.v1.json.
import { readFileSync } from 'node:fs'
import path from 'node:path'
import { MODULE_DIR } from './config.mjs'

export const MEDIA_MANIFEST = path.join(MODULE_DIR, 'duif/config/media-manifest.v1.json')
export const MODULE_MEDIA = path.join(MODULE_DIR, 'duif/frontend/public/media')

// fieldSounds: per band (calm, insistent, alarm) het bestand en de herkomst.
export function fieldSounds(manifest = JSON.parse(readFileSync(MEDIA_MANIFEST, 'utf8'))) {
  const sounds = manifest.native?.sounds ?? {}
  return Object.fromEntries(Object.entries(sounds).map(([band, s]) => [band, { band, file: s.file, sha256: s.sha256, bytes: s.bytes, durationMs: s.durationMs, license: s.rights?.license, attribution: s.attribution, approval: s.approval }]))
}

// bandFiles: band → bestandsnaam, voor meetingReaction.
export function bandFiles(sounds = fieldSounds()) {
  return Object.fromEntries(Object.values(sounds).map((s) => [s.band, s.file]))
}
