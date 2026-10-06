import { fireEvent, render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'
import { describe, expect, it, vi } from 'vitest'
import { serviceContext, testServices } from '../testing/chat'
import { scriptedFetch } from '../testing/fetch'
import { meetingRoutes } from '../testing/meeting'
import LazyMeeting from './LazyMeeting.svelte'
import MeetingView from './MeetingView.svelte'

describe('deferred meeting view', () => {
  it('recovers an unavailable view without starting a meeting or requesting microphone input', async () => {
    const routes = scriptedFetch(meetingRoutes())
    const services = testServices(routes.fetch)
    const loadMeeting = vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValue({ default: MeetingView })
    render(LazyMeeting, { props: { loadMeeting }, context: serviceContext(services) })
    expect(await screen.findByRole('alert')).toHaveTextContent('could not be opened')
    await fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByTestId('seat-button')).toBeVisible()
    expect(services.meeting.phase).toBe('setup')
    expect(routes.calls.filter((r) => r.init?.method === 'POST')).toHaveLength(0)
    services.dispose()
  })
  it('does not mount a late view after disposal', async () => {
    const routes = scriptedFetch(meetingRoutes())
    const services = testServices(routes.fetch)
    let resolve!: (value: { default: typeof MeetingView }) => void
    const promise = new Promise<{ default: typeof MeetingView }>((done) => { resolve = done })
    const ui = render(LazyMeeting, { props: { loadMeeting: () => promise }, context: serviceContext(services) })
    await ui.unmount()
    resolve({ default: MeetingView })
    await tick()
    expect(screen.queryByTestId('seat-button')).toBeNull()
    expect(routes.calls).toHaveLength(0)
    services.dispose()
  })
})
