export type PigeonIdentity = 'biological' | 'individualist' | 'nonverbal'
export type ImageAsset = { id: string; url: string; width: number; height: number; focus: [number, number] }
export type MotionAsset = ImageAsset & { durationMs: number }
export type MediaAttribution = { title: string; creator: string; source: string; license: string; licenseUrl: string; changes: string }
// De kijkhoek per duif is data, geen code: dezelfde getallen sturen de
// Blender-camera van de rustposter en de three.js-camera in de vergadering.
// Azimut en elevatie in radialen in het GLB-frame van pigeonFraming.ts (duif
// kijkt naar −X): 0 is zijaanzicht vanaf +Z met de snavel links, −π/2 recht van
// voren, onder −π/2 kijkt de camera vanaf −Z en wijst de snavel naar rechts.
// Een duif die naar rechts moet kijken krijgt dus een andere azimut, geen
// spiegeling: de das, de textuur en het licht blijven van de echte kant.
// `facing: 'right'` (spiegelen) bestaat alleen nog voor ontwerpweergaven.
export type PigeonView = { azimuth: number; elevation: number; fov: number; facing?: 'left' | 'right' }
export type ModelAsset = { id: string; url: string; posterUrl: string; view: PigeonView; attribution: MediaAttribution }
export type SignalSoundAsset = { id: string; url: string; durationMs: number; attribution: MediaAttribution }
// De drie banden zijn technische variatie in het materiaal, geen emotie en geen
// uitspraak over wat een opname betekent; ze staan nergens in de deelnemertekst.
export type SignalBand = 'calm' | 'insistent' | 'alarm'
export const SIGNAL_BANDS: readonly SignalBand[] = ['calm', 'insistent', 'alarm']
export type SignalSounds = Record<SignalBand, SignalSoundAsset>
// `portraits` is de kaartuitsnede (kop, hals en borst) en `headshots` de krappe
// kopuitsnede naast een chatbeurt: hetzelfde beeld op een andere afstand, zodat
// een portret van 37 px niet dezelfde bytes hoeft te laden als een van 288 px.
// `models` houdt per duif een eigen GLB met rustposter en kijkhoek;
// ontbreekt een duif, dan staat haar portret op het podium.
// De uitlegvideo bovenaan de help is Engelstalig in beide
// interfacetalen; de ondertitels zijn optioneel en even Engels als de video.
// `temporary` is de placeholder: de help beschrijft dan geen inhoud of duur
// die er nog niet is.
export type ExplainerVideo = { id: string; url: string; posterUrl: string; captionsUrl: string | null; width: number; height: number; durationMs: number; language: 'en'; temporary: boolean }
export type PublicMedia = { portraits: Partial<Record<PigeonIdentity, ImageAsset>>; headshots: Partial<Record<PigeonIdentity, ImageAsset>>; rest: ImageAsset | null; motions: MotionAsset[]; temporarySound: boolean; models?: Partial<Record<PigeonIdentity, ModelAsset>>; signalSounds?: SignalSounds | null; explainer?: ExplainerVideo | null }
