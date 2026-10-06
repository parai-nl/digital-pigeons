import type { MsgKey } from '../i18n.svelte'

// PIGEONS is de canonieke duivenlijst van de schil: rol en volgnummer van de
// drie duiven, in de volgorde waarin ze bovenaan staan. PigeonSelector tekent
// hem en HelpView loopt hem af.
//
// De naam staat hier bewust níét meer. Die komt uit
// `config/personas.yaml` via GET /v1/duiven en wordt door de RosterStore
// (roster.svelte.ts) aan de schil geleverd; hernoemen gebeurt daarmee op één
// plek, in de serverconfiguratie, en niet in drie schermen tegelijk. De rol is
// interfacetekst en blijft i18n.
//
// De lijst met wát een duif kan (chat, stem, kennisbank) komt ook van de server
// (lib/pigeons.ts); identiteit en beschikbaarheid blijven gescheiden.
export const PIGEONS = [
  { id: 'biological', role: 'shell.role.biological', number: '01' },
  { id: 'individualist', role: 'shell.role.individualist', number: '02' },
  { id: 'nonverbal', role: 'shell.role.nonverbal', number: '03' },
] as const satisfies readonly { id: string; role: MsgKey; number: string }[]

export type PigeonId = (typeof PIGEONS)[number]['id']
