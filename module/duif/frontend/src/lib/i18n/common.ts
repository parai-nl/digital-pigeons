// Gedeelde teksten (Engels eerst, Nederlands als optie). Sleutels
// zijn Engels en dot-genamespaced; de Nederlandse tabel is getypeerd op de
// Engelse, zodat svelte-check ontbrekende of overbodige sleutels afkeurt.
export const commonEn = {

  "shell.signal.existing": "Go to the current meeting with {pigeon}",

  "shell.choosePigeon": "Choose a pigeon",
  // De duifnaam zelf staat niet meer in dit woordenboek: hij komt uit
  // personas.yaml via GET /v1/duiven (lib/shell/roster.svelte.ts).
  // Deze twee regels zijn wat de schil zegt zolang die naam er nog niet is.
  "shell.nameLoading": "Loading the pigeon’s name…",
  "shell.nameUnavailable": "Pigeon name unavailable",
  "shell.role.biological": "Translates available research",
  "shell.role.individualist": "Brings the pigeon’s interest to the table",
  "shell.role.nonverbal": "A presence without words",
  "shell.mode": "Choose how to meet",
  "shell.nav": "Views and help",
  "shell.help": "About & help",
  "shell.site": "Project site",
  "shell.closeHelp": "Back to the experience",
  "shell.helpClose": "Close help",
  "shell.helpLoading": "Opening the guide…",
  "shell.helpFailed": "The guide could not be opened. Try again or return to your conversation.",
  "shell.back": "Back to {view}",
  "shell.languageLocked": "The meeting language stays {language}. End the meeting to change it.",
  "shell.signal.title": "Some things enter a conversation without words.",
  "shell.signal.body": "The Signaling Pigeon joins a meeting through movement and sound. There is no chat or translation of the signal. You decide what its presence does to the conversation.",
  "shell.signal.action": "Bring this pigeon to a meeting",
  "shell.signal.note": "Your microphone only starts after you choose to begin the meeting.",
  "shell.session": "At the table",
  "shell.session.stop": "Stop contribution",
  "shell.session.micOn": "Microphone on",
  "shell.session.micOff": "Microphone off",
  "shell.session.micOpening": "Opening microphone…",
  "shell.session.micRefused": "Microphone unavailable",

  'common.tab.chat': 'Chat',
  'common.tab.meeting': 'Meeting',
  'common.connection.lost': 'Connection lost.',
  'common.pigeons.unavailable': 'Could not load the pigeons — are you logged in?',
  // De keuzelijst van @parai/ui heeft Engelse standaardteksten; die horen bij
  // de gastheer, niet bij de component.
  'common.select.placeholder': 'Choose…',
  'common.select.search': 'Type to search…',
  // De stand van de kennisbank-import (ImportState), gedeeld door chat en graaf.
  // De teller telt rijen uit de `destillaat`-tabel; op het scherm heten die
  // gewoon bronnen. De tabelnaam en het woord "standpunt" horen niet in de
  // chat van de kennispartner thuis.
  'knowledge.state.importing': 'The pigeon is still reading her knowledge base: {count} sources so far…',
  'knowledge.state.importing.start': 'The pigeon is still reading her knowledge base…',
  'knowledge.state.incomplete': 'The knowledge base is incomplete; the pigeon may be missing sources.',
  'knowledge.state.failed': 'The knowledge base could not be read. Source support is unavailable.',
  'knowledge.state.empty': 'No knowledge base imported yet.',
  'error.kb_unavailable':
    'The pigeon cannot reach her knowledge base right now, so she is not answering: without her sources she would be guessing. Please try again in a moment.',
  'error.llm_budget_exceeded':
    'The pigeons have reached their monthly budget. They are resting; please try again later or contact the team.',
  'error.llm_unavailable': 'The pigeons cannot reach their language model right now. Please try again in a moment.',
  'error.internal_error': 'The pigeon lost its train of thought. The button still works — please try again.',
  'error.unauthorized': 'Login required.',
  // Terugval als de server zijn eigen zin bij een 400 niet meestuurt: de
  // reden staat dan alleen in de code invalid_request.
  'error.invalid_request': 'That request could not be processed. Reload the page and try again.',
  // De harde tijdgrens van een beurt. De server stuurt zijn eigen zin mee; deze
  // staat er voor de gevallen waarin alleen de code overkomt.
  'error.turn_timeout':
    'This answer took too long and was stopped. The button still works — try again, ideally with a shorter or more specific question.',
  'error.generic': 'Something went wrong. Please try again.',
} as const

export const commonNl: Record<keyof typeof commonEn, string> = {

  "shell.signal.existing": "Naar de lopende vergadering met {pigeon}",

  "shell.choosePigeon": "Kies een duif",
  "shell.nameLoading": "De naam van de duif wordt geladen…",
  "shell.nameUnavailable": "Duifnaam niet beschikbaar",
  "shell.role.biological": "Vertaalt beschikbaar onderzoek",
  "shell.role.individualist": "Brengt het belang van de duif in",
  "shell.role.nonverbal": "Aanwezig zonder woorden",
  "shell.mode": "Kies hoe je de duif ontmoet",
  "shell.nav": "Weergaven en hulp",
  "shell.help": "Uitleg & hulp",
  "shell.site": "Projectsite",
  "shell.closeHelp": "Terug naar de ervaring",
  "shell.helpClose": "Uitleg sluiten",
  "shell.helpLoading": "De uitleg wordt geopend…",
  "shell.helpFailed": "De uitleg kon niet worden geopend. Probeer opnieuw of keer terug naar je gesprek.",
  "shell.back": "Terug naar {view}",
  "shell.languageLocked": "De vergadertaal blijft {language}. Beëindig de vergadering om deze te wijzigen.",
  "shell.signal.title": "Soms komt iets zonder woorden het gesprek binnen.",
  "shell.signal.body": "De signalerende duif doet mee aan een vergadering met beweging en geluid. Er is geen chat of vertaling van het signaal. Je ontdekt zelf wat haar aanwezigheid met het gesprek doet.",
  "shell.signal.action": "Neem deze duif mee naar een vergadering",
  "shell.signal.note": "Je microfoon start pas wanneer je bewust de vergadering begint.",
  "shell.session": "Aan tafel",
  "shell.session.stop": "Stop inbreng",
  "shell.session.micOn": "Microfoon aan",
  "shell.session.micOff": "Microfoon uit",
  "shell.session.micOpening": "Microfoon openen…",
  "shell.session.micRefused": "Microfoon niet beschikbaar",

  'common.tab.chat': 'Chat',
  'common.tab.meeting': 'Vergadering',
  'common.connection.lost': 'Verbinding verbroken.',
  'common.pigeons.unavailable': 'Kon de duiven niet laden — ben je ingelogd?',
  'common.select.placeholder': 'Kies…',
  'common.select.search': 'Typ om te zoeken…',
  'knowledge.state.importing': 'De duif leest de kennisbank nog: {count} bronnen…',
  'knowledge.state.importing.start': 'De duif leest de kennisbank nog…',
  'knowledge.state.incomplete': 'De kennisbank is onvolledig; de duif mist mogelijk bronnen.',
  'knowledge.state.failed': 'De kennisbank kon niet gelezen worden. Brononderbouwing is niet beschikbaar.',
  'knowledge.state.empty': 'Nog geen kennisbank geïmporteerd.',
  'error.kb_unavailable':
    'De duif kan haar kennisbank nu niet bereiken en antwoordt daarom niet: zonder haar bronnen zou ze gokken. Probeer het zo weer.',
  'error.llm_budget_exceeded':
    'De duiven hebben hun maandbudget bereikt. Ze rusten even; probeer het later of neem contact op met het team.',
  'error.llm_unavailable': 'De duiven kunnen hun taalmodel nu niet bereiken. Probeer het zo weer.',
  'error.internal_error': 'De duif verloor de draad. De knop werkt nog — probeer het opnieuw.',
  'error.unauthorized': 'Inloggen vereist.',
  'error.invalid_request': 'Dat verzoek kon niet verwerkt worden. Herlaad de pagina en probeer het opnieuw.',
  'error.turn_timeout':
    'Dit antwoord duurde te lang en is gestopt. De knop werkt nog — probeer het opnieuw, het liefst met een kortere of concretere vraag.',
  'error.generic': 'Er ging iets mis. Probeer het opnieuw.',
}
