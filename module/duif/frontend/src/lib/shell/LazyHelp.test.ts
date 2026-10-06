import { fireEvent, render, screen } from '@testing-library/svelte'
import { describe, expect, it, vi } from 'vitest'
import { UiLocale } from '../i18n.svelte'
import { loadedRoster } from '../testing/chat'
import HelpView from './HelpView.svelte'
import LazyHelp from './LazyHelp.svelte'
import { helpEn, helpNl } from './helpCopy'
import { readingEn, readingNl } from '../graph/readingCopy'

describe('deferred help', () => {
  it('keeps a way back during failure and restores the guide on explicit retry', async () => {
    const loader = vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValue({ default: HelpView })
    const locale = new UiLocale()
    const roster = await loadedRoster(locale)
    render(LazyHelp, { locale, roster, onclose: vi.fn(), ongraph: vi.fn(), loadHelp: loader })
    expect(await screen.findByRole('alert')).toHaveTextContent('could not be opened')
    // De focus zit in het venster zelf (HelpDialog); wat hier telt is
    // dat er ook tijdens een mislukte laadpoging een weg terug staat.
    expect(screen.getByRole('button', { name: 'Back to the experience' })).toBeInTheDocument()
    await fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByTestId('help-view')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Back to the experience/ })).toBeInTheDocument()
    expect(loader).toHaveBeenCalledTimes(2)
  })

  it('keeps the lazy reading dictionaries complete in both languages', () => {
    for (const [en, nl] of [[helpEn, helpNl], [readingEn, readingNl]]) {
      expect(Object.keys(en).sort()).toEqual(Object.keys(nl).sort())
      for (const [key, text] of Object.entries(en)) {
        expect(text.trim(), key).not.toBe('')
        const translated = (nl as Record<string, string>)[key]
        expect(translated.trim(), key).not.toBe('')
        const fields = (value: string) => [...value.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort()
        expect(fields(translated), key).toEqual(fields(text))
      }
    }
  })
})
