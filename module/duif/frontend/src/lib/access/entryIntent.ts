import type { Services } from '../services.svelte'
import { CONVERSATION_CASES, GAP_CASE } from '../chat/cases'

// Login return_to carries only public, allowlisted identifiers. Neither a
// transcript nor arbitrary question text is accepted from the URL. Restoring
// a case prepares a draft; it never sends a question or opens the microphone.
export async function applyEntryIntent(services: Services, search: string): Promise<void> {
  const params = new URLSearchParams(search)
  const pigeon = params.get('pigeon')
  const example = [...CONVERSATION_CASES, GAP_CASE].find((item) => item.id === params.get('case'))
  if (!pigeon && !example) return
  await services.chat.loadPigeons()
  if (services.chat.disposed || services.chat.draft || services.chat.sessionId) return
  if (pigeon === 'nonverbal') { services.shell.signaling = true; return }
  if (pigeon && services.chat.pigeons.some((item) => item.id === pigeon)) services.chat.selectPigeon(pigeon)
  if (example) services.chat.draft = example.question[services.locale.lang]
}
