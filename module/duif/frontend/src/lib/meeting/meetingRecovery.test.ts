import { afterEach, expect, it } from 'vitest';
import { jsonResponse, scriptedFetch, type Route } from '../testing/fetch';
import { meetingHarness, meetingRoutes } from '../testing/meeting';
import { SETTINGS_DEBOUNCE_MS } from './meetingStore.svelte';
const dispose: {
    dispose(): void;
}[] = [];
afterEach(() => { for (const store of dispose.splice(0))
    store.dispose(); });
const settle = () => new Promise(r => setTimeout(r, 0));
async function live(routes: Route[] = []) {
    const scripted = scriptedFetch([...routes, ...meetingRoutes()]);
    const h = meetingHarness(scripted.fetch);
    dispose.push(h.store);
    await h.store.loadPigeons();
    await h.store.start();
    await settle();
    h.ports.channel().emit({ type: 'state', sessionId: 'sessie-1', turnId: '0', voice: 'ready', listening: 'listening', speaking: false, playbackTracking: true, settings: { autonomous: true, turnId: '0' } });
    return { ...h, calls: scripted.calls };
}
it('stops buffered PCM locally on a frame gap even when playback acknowledgement fails', async () => {
    const h = await live([{ method: 'POST', match: '/playback', respond: () => Promise.reject(new Error('offline')) }]);
    const ch = h.ports.channel();
    ch.emit({ type: 'persona', sessionId: 'sessie-1', turnId: '1', pigeonId: 'individualist' });
    ch.emit({ type: 'tts_audio', turnId: '1', format: 'pcm16', seq: 1, audio: 'AQACAA==', sampleRate: 24000 });
    const player = h.ports.context().stream!;
    ch.emit({ type: 'tts_audio', turnId: '1', format: 'pcm16', seq: 3, audio: 'AwAEAA==', sampleRate: 24000 });
    ch.emit({ type: 'tts_done', turnId: '1', frames: 3 });
    ch.emit({ type: 'response_done', turnId: '1' });
    await settle();
    expect(h.store.audioGap).toBe(true);
    expect(h.store.speaking).toBe(false);
    expect(player.stopped).toBe(true);
    expect(h.store.autonomyPaused).toBe(true);
    expect(h.calls.filter(c => c.url.endsWith('/playback')).map(c => (c.body as {
        state: string;
    }).state)).toEqual(['started', 'failed']);
    player.finishPlayback();
    ch.emit({ type: 'tts_audio', turnId: '1', format: 'pcm16', seq: 4, audio: 'AwAEAA==', sampleRate: 24000 });
    expect(h.store.speaking).toBe(false);
    expect(h.ports.context().stream).toBe(player);
});
it('sends the Stop acknowledgement barrier on the explicit resume request', async () => {
    const h = await live([{ method: 'POST', match: '/stop', respond: () => jsonResponse({ stopped: true, autonomyPaused: true, settings: { turnId: '2', autonomyPaused: true } }, 202) }]);
    await h.store.stop();
    await h.store.resumeAutonomy();
    expect(h.calls.find(c => c.url.endsWith('/resume'))?.body).toEqual({ turnId: '2' });
    expect(h.store.autonomyPaused).toBe(false);
});
it('does not claim resume success without the matching server barrier', async () => {
    const h = await live([{ method: 'POST', match: '/resume', respond: () => jsonResponse({ autonomyPaused: false, settings: { turnId: '9' } }, 202) }]);
    await h.store.stop();
    await h.store.resumeAutonomy();
    expect(h.store.autonomyPaused).toBe(true);
    expect(h.store.playbackSyncFailed).toBe(true);
});
it('can explicitly retry after a conflict using the current server barrier', async () => {
    let count = 0;
    const h = await live([{ method: 'POST', match: '/resume', respond: ({ body }) => ++count === 1
                ? jsonResponse({ error: 'meeting_control_changed', settings: { turnId: '2', autonomyPaused: true } }, 409)
                : jsonResponse({ autonomyPaused: false, settings: { turnId: (body as {
                            turnId: string;
                        }).turnId } }, 202) }]);
    await h.store.stop();
    await h.store.resumeAutonomy();
    expect(h.store.autonomyPaused).toBe(true);
    await h.store.resumeAutonomy();
    expect(h.calls.filter(c => c.url.endsWith('/resume')).map(c => c.body)).toEqual([{ turnId: '0' }, { turnId: '2' }]);
    expect(h.store.autonomyPaused).toBe(false);
});
it('does not turn a pending older settings toggle into a resume after a newer Stop', async () => {
    const h = await live([{ method: 'POST', match: '/settings', respond: () => jsonResponse({ autonomous: true, autonomyPaused: true, turnId: '2' }, 202) }]);
    await h.store.stop();
    h.store.updateSettings({ autonomous: false });
    h.store.updateSettings({ autonomous: true });
    await h.store.stop();
    h.ports.advance(SETTINGS_DEBOUNCE_MS);
    await settle();
    expect(h.calls.filter(c => c.url.endsWith('/resume'))).toHaveLength(0);
    expect(h.store.autonomyPaused).toBe(true);
});
it('uses the current settings barrier when an explicit off-to-on toggle resumes', async () => {
    const h = await live([{ method: 'POST', match: '/settings', respond: () => jsonResponse({ autonomous: true, autonomyPaused: true, turnId: '2' }, 202) }]);
    await h.store.stop();
    h.store.updateSettings({ autonomous: false });
    h.store.updateSettings({ autonomous: true });
    h.ports.advance(SETTINGS_DEBOUNCE_MS);
    await settle();
    expect(h.calls.find(c => c.url.endsWith('/resume'))?.body).toEqual({ turnId: '2' });
    expect(h.store.autonomyPaused).toBe(false);
});
it('does not consume an upcoming persona event when a settings snapshot already saw its turn', async () => {
    const h = await live();
    h.ports.channel().emit({ type: 'settings', settings: { turnId: '1', autonomyPaused: false } });
    h.ports.channel().emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' });
    h.ports.channel().emit({ type: 'tts_audio', turnId: '1', format: 'pcm16', seq: 1, audio: 'AQACAA==', sampleRate: 24000 });
    expect(h.store.speaking).toBe(true);
    expect(h.ports.context().stream?.stopped).toBe(false);
});

it('stops server-failed synthesis immediately but retains later text without a canceling acknowledgement', async () => {
    const h = await live();
    const ch = h.ports.channel();
    await h.store.respond();
    ch.emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' });
    ch.emit({ type: 'text', turnId: '1', delta: 'The first written sentence. ' });
    ch.emit({ type: 'tts_audio', turnId: '1', format: 'pcm16', seq: 1, audio: 'AQACAA==', sampleRate: 24000 });
    const player = h.ports.context().stream!;
    ch.emit({ type: 'tts_done', turnId: '1', frames: 1, error: 'tts_incomplete' });
    await settle();
    expect(player.stopped).toBe(true);
    expect(h.store.audioGap).toBe(true);
    ch.emit({ type: 'listening_heartbeat', speaking: true, listening: 'listening' });
    ch.emit({ type: 'playback_state', turnId: '1', speaking: true, playback: 'playing' });
    expect(h.store.speaking).toBe(false);
    expect(h.calls.filter(c => c.url.endsWith('/playback')).map(c => (c.body as {state:string}).state)).toEqual(['started']);
    ch.emit({ type: 'tts_audio', turnId: '1', format: 'pcm16', seq: 2, audio: 'AwAEAA==', sampleRate: 24000 });
    expect(h.ports.context().stream).toBe(player);
    ch.emit({ type: 'text', turnId: '1', delta: 'The final written sentence.' });
    ch.emit({ type: 'response_done', turnId: '1', kbStatus: 'gap', sources: [] });
    await settle();
    expect(h.store.answers.at(-1)?.text).toBe('The first written sentence. The final written sentence.');
    expect(h.store.answers.at(-1)?.status).toBe('complete');
    expect(h.store.autonomyPaused).toBe(true);
    expect(h.calls.filter(c => c.url.endsWith('/playback')).map(c => (c.body as {state:string}).state)).toEqual(['started']);
});

it('allows a fresh automatic turn after a confirmed context change while rejecting old frames', async () => {
    const h = await live();
    const ch = h.ports.channel();
    ch.emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' });
    ch.emit({ type: 'text', turnId: '1', delta: 'A superseded answer.' });
    ch.emit({ type: 'stopped', turnId: '2', reason: 'context_changed', autonomyPaused: false });
    ch.emit({ type: 'settings', settings: { turnId: '2', autonomyPaused: false } });
    expect(h.store.autonomyPaused).toBe(false);
    expect(h.store.answers.at(-1)?.status).toBe('interrupted');
    ch.emit({ type: 'text', turnId: '1', delta: 'Stale text.' });
    ch.emit({ type: 'tts_audio', turnId: '1', format: 'pcm16', seq: 1, audio: 'AQACAA==', sampleRate: 24000 });
    expect(h.ports.context().stream).toBeUndefined();
    expect(h.store.answers.at(-1)?.text).toBe('A superseded answer.');
    ch.emit({ type: 'persona', turnId: '3', pigeonId: 'individualist' });
    ch.emit({ type: 'text', turnId: '3', delta: 'A fresh answer.' });
    expect(h.store.answers.at(-1)?.text).toBe('A fresh answer.');
    expect(h.store.speaking).toBe(true);
    expect(h.calls.filter(c => c.url.endsWith('/playback'))).toHaveLength(0);
});
it('never treats an automatic context change as consent to release a locally held Stop', async () => {
    const h = await live();
    const ch = h.ports.channel();
    ch.emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' });
    await h.store.stop();
    ch.emit({ type: 'stopped', turnId: '2', reason: 'context_changed', autonomyPaused: false });
    ch.emit({ type: 'settings', settings: { turnId: '2', autonomyPaused: false } });
    ch.emit({ type: 'persona', turnId: '3', pigeonId: 'individualist' });
    expect(h.store.autonomyPaused).toBe(true);
    expect(h.store.speaking).toBe(false);
    expect(h.store.answers).toHaveLength(1);
});

// Begon de stem van een beurt nooit (tts_unavailable), dan speelde er
// niets en kan het geluid ook niet "niet volledig" zijn afgespeeld. De melding
// zegt wat er wél is: geen stem, wel de tekst.
it('says the voice is unavailable, not that the sound did not play in full, when speech never started', async () => {
    const h = await live();
    const ch = h.ports.channel();
    await h.store.respond();
    ch.emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' });
    ch.emit({ type: 'text', turnId: '1', delta: 'The written sentence.' });
    ch.emit({ type: 'tts_done', turnId: '1', frames: 0, error: 'tts_unavailable' });
    ch.emit({ type: 'response_done', turnId: '1', kbStatus: 'gap', sources: [] });
    await settle();
    expect(h.store.audioGap).toBe(false);
    expect(h.store.notice?.id).toBe('voice-unavailable');
    expect(h.store.notice?.text).toBe('The voice is not available right now. The text is on screen.');
    expect(h.store.answers.at(-1)?.text).toBe('The written sentence.');

    // De volgende beurt begint zonder die melding.
    await h.store.respond();
    ch.emit({ type: 'persona', turnId: '2', pigeonId: 'individualist' });
    expect(h.store.notice?.id).not.toBe('voice-unavailable');
});

// Klonk er al iets en stokte het daarna, dan blijft het een gat in het geluid.
it('keeps the incomplete-sound notice when audio had already played before the voice failed', async () => {
    const h = await live();
    const ch = h.ports.channel();
    await h.store.respond();
    ch.emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' });
    ch.emit({ type: 'tts_audio', turnId: '1', format: 'pcm16', seq: 1, audio: 'AQACAA==', sampleRate: 24000 });
    ch.emit({ type: 'tts_done', turnId: '1', frames: 1, error: 'tts_unavailable' });
    await settle();
    expect(h.store.audioGap).toBe(true);
    expect(h.store.notice?.id).toBe('sound');
});

// De beurtklok is geen afspeelfout. De server pauzeert er niet op,
// dus de client ook niet: de knop en het zelfstandig meedoen blijven zoals ze
// waren. Een gewone fout pauzeert wel, zoals voorheen.
it('does not pause locally when the turn clock stopped a turn', async () => {
    const h = await live();
    const ch = h.ports.channel();
    await h.store.respond();
    ch.emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' });
    ch.emit({ type: 'text', turnId: '1', delta: 'Half a sentence' });
    ch.emit({ type: 'error', turnId: '1', error: 'turn_timeout', message: { en: 'The pigeon took too long and was stopped.', nl: 'De duif deed er te lang over en is gestopt.' } });
    ch.emit({ type: 'response_done', turnId: '1', stopReason: 'turn_timeout' });
    await settle();
    expect(h.store.speaking).toBe(false);
    expect(h.store.autonomyPaused).toBe(false);
    expect(h.store.errorText).toBe('The pigeon took too long and was stopped.');
    expect(h.store.answers.at(-1)?.status).toBe('interrupted');
});

it('still pauses locally on any other turn failure', async () => {
    const h = await live();
    const ch = h.ports.channel();
    await h.store.respond();
    ch.emit({ type: 'persona', turnId: '1', pigeonId: 'individualist' });
    ch.emit({ type: 'error', turnId: '1', error: 'llm_unavailable', message: { en: 'Unavailable.' } });
    await settle();
    expect(h.store.autonomyPaused).toBe(true);
});
