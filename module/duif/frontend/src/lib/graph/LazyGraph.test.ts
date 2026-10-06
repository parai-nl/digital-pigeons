import { fireEvent, render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'
import { describe, expect, it, vi } from 'vitest'
import { serviceContext, testServices } from '../testing/chat'
import { scriptedFetch } from '../testing/fetch'
import { graphRoutes, GRAPH_IDS } from '../testing/graph'
import GraphView from './GraphView.svelte'
import LazyGraph from './LazyGraph.svelte'

function services() {
  const scripted = scriptedFetch(graphRoutes())
  const services = testServices(scripted.fetch)
  services.graph.visible = true
  return { services, scripted }
}

describe('deferred graph module', () => {
  it('shows an honest failure and retries only explicitly, using the existing source selection', async () => {
    const { services: app } = services()
    await app.graph.focus(GRAPH_IDS.destillaatOne)
    const loader = vi.fn().mockRejectedValueOnce(new Error('chunk unavailable')).mockResolvedValue({ default: GraphView })
    render(LazyGraph, { props: { loadGraph: loader }, context: serviceContext(app) })
    expect(await screen.findByRole('alert')).toHaveTextContent('could not be opened')
    expect(loader).toHaveBeenCalledOnce()
    expect(app.graph.selected).toBe(GRAPH_IDS.destillaatOne)
    await fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByTestId('graph-detail')).toHaveAttribute('data-id', GRAPH_IDS.destillaatOne)
    expect(loader).toHaveBeenCalledTimes(2)
    app.dispose()
  })

  it('does not mount a late chunk or fetch graph data after unmount', async () => {
    const { services: app, scripted } = services()
    let resolve: (value: { default: typeof GraphView }) => void = () => {}
    const pending = new Promise<{ default: typeof GraphView }>((done) => { resolve = done })
    const rendered = render(LazyGraph, { props: { loadGraph: () => pending }, context: serviceContext(app) })
    expect(screen.getByRole('status')).toHaveTextContent('Opening the knowledge graph')
    await rendered.unmount()
    resolve({ default: GraphView })
    await tick()
    expect(scripted.calls).toHaveLength(0)
    expect(screen.queryByTestId('graph-view')).toBeNull()
    app.dispose()
  })
})
