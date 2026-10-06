import { render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'
import { describe, expect, it } from 'vitest'
import KnowledgeBanner from './KnowledgeBanner.svelte'
import type { KnowledgeStatus } from './knowledge.svelte'
import { serviceContext, testServices } from './testing/chat'
import { scriptedFetch } from './testing/fetch'

function mount(status: KnowledgeStatus | null) {
  const services = testServices(scriptedFetch([]).fetch)
  services.knowledge.status = status
  return { services, ...render(KnowledgeBanner, { context: serviceContext(services) }) }
}

describe('KnowledgeBanner', () => {
  it('counts the sources while the pigeon is still reading', () => {
    mount({ state: 'importing', counts: { destillaat: 1200 } })
    expect(screen.getByTestId('knowledge-banner')).toHaveTextContent(
      'The pigeon is still reading her knowledge base: 1200 sources so far…',
    )
  })

  it('says the pigeon is reading even before the first count arrives', () => {
    mount({ state: 'importing' })
    expect(screen.getByTestId('knowledge-banner')).toHaveTextContent('The pigeon is still reading her knowledge base…')
  })

  it('warns about an incomplete knowledge base', () => {
    mount({ state: 'incomplete' })
    const banner = screen.getByTestId('knowledge-banner')
    expect(banner).toHaveAttribute('data-state', 'incomplete')
    expect(banner).toHaveTextContent('The knowledge base is incomplete')
  })

  it('renders a failure reason through i18n and never as a raw code', () => {
    mount({ state: 'failed', reason: 'count_mismatch' })
    expect(screen.getByTestId('knowledge-banner')).toHaveTextContent('The knowledge base could not be read.')
    expect(screen.getByTestId('knowledge-banner').textContent).not.toContain('count_mismatch')
  })

  it('uses a known reason code as its own message', () => {
    mount({ state: 'failed', reason: 'kb_unavailable' })
    expect(screen.getByTestId('knowledge-banner')).toHaveTextContent('cannot reach her knowledge base')
  })

  it('says there is no knowledge base yet', () => {
    mount({ state: 'empty' })
    expect(screen.getByTestId('knowledge-banner')).toHaveTextContent('No knowledge base imported yet.')
  })

  it('hangs no banner when the knowledge base is ready or unknown', async () => {
    const { services } = mount(null)
    expect(screen.queryByTestId('knowledge-banner')).toBeNull()
    services.knowledge.status = { state: 'ready', counts: { destillaat: 4067 } }
    await tick()
    expect(screen.queryByTestId('knowledge-banner')).toBeNull()
  })

  it('follows the ui language', async () => {
    const { services } = mount({ state: 'empty' })
    services.locale.choose('nl')
    await tick()
    expect(screen.getByTestId('knowledge-banner')).toHaveTextContent('Nog geen kennisbank geïmporteerd.')
  })
})
