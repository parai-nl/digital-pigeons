<script lang="ts">
  import { useServices } from '../services.svelte'
  import { shortTime, type SessionRef } from './chatStore.svelte'

  // De lijst met bewaarde gesprekken van de gekozen duif. Een gewone lijst
  // onder "Historie": hij blijft staan tot je een gesprek kiest, en hij vult
  // zichzelf niet opnieuw op het moment dat je hem opent — dat verving de
  // rijen onder de aanwijzer terwijl je aan het klikken was.
  let {
    sessions,
    title,
    onopen,
    ondelete,
  }: {
    sessions: SessionRef[]
    title: (session: SessionRef) => string
    onopen: (session: SessionRef) => void
    ondelete: (session: SessionRef, trigger: HTMLElement) => void
  } = $props()

  const { chat, locale, roster } = useServices()

</script>

<nav id="chat-history" class="chat-history" aria-label={locale.t('chat.sessions.title')}>
  <div class="history-top">
    <p>{locale.t('chat.history.scope')}</p>
    <button type="button" class="duif-text-action" disabled={chat.loadingSessions} onclick={() => void chat.loadSessions(true)}>
      {locale.t(chat.loadingSessions ? 'chat.history.refreshing' : 'chat.history.refresh')}
    </button>
  </div>
  {#if chat.sessionsError}<p role="alert">{locale.t('chat.history.failed')}</p>{/if}
  <ul data-testid="session-list">
    {#each sessions as session (session.id)}
      <li>
        <button
          type="button"
          aria-current={session.id === chat.sessionId ? 'true' : undefined}
          aria-label={`${locale.t('chat.sessions.open', { pigeon: roster.name(session.pigeonId) })}: ${title(session)} · ${shortTime(session.updatedAt, locale.lang)}`}
          onclick={() => onopen(session)}
        >
          <span>{#if session.comparison}<small>{locale.t('chat.history.comparison')} · </small>{/if}{title(session)}</span>
          <time datetime={session.updatedAt}>{shortTime(session.updatedAt, locale.lang)}</time>
        </button>
        <button
          type="button"
          class="duif-danger-action"
          aria-label={`${locale.t('chat.delete.action')}: ${title(session)}`}
          onclick={(event) => ondelete(session, event.currentTarget)}
        >{locale.t('chat.delete.action')}</button>
      </li>
    {:else}
      {#if !chat.loadingSessions && !chat.sessionsError}<li data-testid="sessions-empty">{locale.t('chat.sessions.empty')}</li>{/if}
    {/each}
  </ul>
</nav>

<style>
  .chat-history { padding: .6rem .8rem .8rem; border: 1px solid var(--duif-line); background: var(--duif-paper); border-radius: .6rem; margin-top: .6rem; }
  /* Bereik en verversen staan op één regel: de lijst zelf is waar het om gaat. */
  .history-top { display: flex; align-items: baseline; justify-content: space-between; gap: .8rem; }
  .chat-history p { color: var(--duif-dim); font-size: .75rem; margin: 0; }
  .chat-history .duif-text-action { min-height: 1.9rem; font-size: .75rem; }
  .chat-history ul { max-height: 15rem; overflow-y: auto; padding: .2rem; margin-top: .2rem; }
  .chat-history li { display: flex; align-items: baseline; gap: .4rem; }
  .chat-history li > button:first-child { display: flex; flex: 1; min-width: 0; gap: 1rem; align-items: baseline; padding: .6rem; text-align: left; border-radius: .3rem; font-size: .85rem; cursor: pointer; }
  .chat-history li > button:first-child[aria-current='true'], .chat-history li > button:first-child:hover { background: var(--duif-feather); }
  .chat-history li > button:first-child span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .chat-history time { margin-left: auto; flex-shrink: 0; white-space: nowrap; font-size: .7rem; color: var(--duif-dim); }
  .chat-history .duif-danger-action { flex-shrink: 0; }
  button:disabled { opacity: .5; cursor: default; }
  button:focus-visible { outline: 2px solid var(--duif-ink); outline-offset: 3px; }
</style>
