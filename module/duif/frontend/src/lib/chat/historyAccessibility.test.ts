import { afterEach, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import { tick } from 'svelte';
import { pigeonRoute, serviceContext, testServices } from '../testing/chat';
import { jsonResponse, scriptedFetch } from '../testing/fetch';
import ConversationActions from '../shell/ConversationActions.svelte';
afterEach(cleanup);
it('makes saved conversations distinguishable by their title for screen readers', async () => {
    const scripted = scriptedFetch([pigeonRoute(), { method: 'GET', match: '/chat/sessions', respond: () => jsonResponse({ sessions: [{ id: 'first', pigeonId: 'biological', title: 'First topic', lang: 'en', createdAt: '2026-09-07', updatedAt: '2026-09-07' }, { id: 'second', pigeonId: 'biological', title: 'Second topic', lang: 'en', createdAt: '2026-09-07', updatedAt: '2026-09-07' }] }) }]);
    const services = testServices(scripted.fetch);
    try {
        render(ConversationActions, { context: serviceContext(services) });
        await services.chat.loadPigeons();
        await services.chat.loadSessions();
        await new Promise(r => setTimeout(r, 0));
        await tick();
        await fireEvent.click(screen.getByRole('button', { name: /history/i }));
        await tick();
        expect(screen.getByText('First topic').closest('button')).toHaveAccessibleName(/First topic/);
        expect(screen.getByText('Second topic').closest('button')).toHaveAccessibleName(/Second topic/);
    }
    finally {
        services.dispose();
    }
});
