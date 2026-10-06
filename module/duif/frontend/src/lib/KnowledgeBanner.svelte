<script lang="ts">
  import Banner from '@parai/ui/components/Banner.svelte'
  import { useServices } from './services.svelte'

  // Eén banner voor de stand van de kennisbank, gedeeld door de chat en de
  // kennisgraaf: leest de duif nog, is de import onvolledig, mislukt of is er
  // nog helemaal geen kennisbank, dan hoort de lezer dat te weten voordat hij
  // een antwoord zonder bronnen wantrouwt. Bij `ready` staat er niets: een
  // banner die altijd hangt, wordt niet meer gelezen.
  // class laat de gastheer de banner in zijn eigen ritme zetten (de chat plaatst
  // hem in een kolom, de graaf tussen de werkbalk en de plattegrond) zonder een
  // lege wrapper met marge achter te laten als er niets te melden is.
  const { class: className = '' }: { class?: string } = $props()

  const { knowledge, locale } = useServices()

  const state = $derived(knowledge.status?.state ?? null)

  // Tijdens de import telt de server per tabel; staat het aantal standpunten
  // er al in, dan is dat de eerlijkste voortgang die we hebben.
  const text = $derived.by(() => {
    const status = knowledge.status
    if (!status) return ''
    switch (status.state) {
      case 'importing': {
        const count = status.counts?.destillaat
        return typeof count === 'number' && count > 0
          ? locale.t('knowledge.state.importing', { count })
          : locale.t('knowledge.state.importing.start')
      }
      case 'incomplete':
        return locale.t('knowledge.state.incomplete')
      // De reden is een code, geen zin: kent de UI hem als foutsleutel, dan
      // vertelt die meer; zo niet, dan blijft het bij de algemene regel.
      case 'failed':
        return locale.fromServer(undefined, status.reason, 'knowledge.state.failed')
      case 'empty':
        return locale.t('knowledge.state.empty')
      default:
        return ''
    }
  })

  const tone = $derived(state === 'failed' ? 'danger' : state === 'incomplete' ? 'warning' : 'secondary')
</script>

{#if text !== ''}
  <Banner
    class={className}
    message={text}
    {tone}
    role="status"
    aria-live="polite"
    data-testid="knowledge-banner"
    data-state={state}
  />
{/if}
