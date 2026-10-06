// Teksten van de chatweergave. Eigenaar: lib/chat. Zelfde typeregel als
// common.ts: nl is getypeerd op en.
//
// Alle chatcopy staat hier, ook de teksten die eerder in losse copy-objecten
// leefden (comparisonCopy.ts, access/deleteCopy.ts). Eén tabel betekent één
// plek waar EN/NL-pariteit door de compiler wordt afgedwongen.
export const chatEn = {
  // De bronlade, van boven naar beneden: één regel met het perspectief, dan het
  // standpunt als hoofdelement, dan de originele woorden, dan pas de
  // brongegevens. Elk blok draagt zijn eigen label. De samenvatting is een
  // redactionele lezing van de collectie, geen citaat: de teksten hieronder
  // zeggen dat, en nodigen uit om het bij de passage na te lezen.
  'chat.source.perspective': 'Perspective',
  'chat.source.finding': 'The standpoint drawn from this source',
  'chat.source.summary': 'This is how the collection summarises the source; read the original passage below to check it.',
  'chat.source.summaryAlone': 'This is how the collection summarises the source; the original passage is not included here to check it against.',
  'chat.source.original': 'The original words in the paper',
  'chat.source.originalPlain': 'The original words',
  'chat.source.unverified': 'This summary has not been checked against the original text.',
  'chat.source.rejected': 'The original passages of this source could not be traced back to the paper, so they are not shown here; the summary is unchecked.',
  // De samenvatting kan een vertaling zijn; het citaat eronder is altijd het
  // origineel. Deze regel staat er alleen als wat de lezer ziet niet de taal
  // van de kennisbank zelf is.
  'chat.source.translated': 'Translated summary; the quotation is the original.',
  "chat.history.toggle": "History",
  "chat.history.refresh": "Refresh history",
  "chat.history.refreshing": "Refreshing history…",
  "chat.history.failed": "History could not be refreshed. Your current conversation is unchanged. Try refreshing again.",
  "chat.history.scope": "Only conversations with this pigeon",
  "chat.history.comparison": "Comparison",
  "chat.history.loading": "Loading your conversation…",
  "chat.starters.title": "Start with a question",
  "chat.starters.intro": "Choose an example and make it your own. You decide when to send it.",
  "chat.starters.question": "Question",
  "chat.starters.case": "Case to discuss",
  "chat.draft.title": "Keep your draft?",
  "chat.draft.body": "You already have text in the input. Choose whether to keep it or replace it with this example.",
  "chat.draft.keep": "Keep my draft",
  "chat.draft.replace": "Use this example",
  "chat.reset.title": "Start a clean conversation?",
  "chat.reset.body": "This stops the current answer and clears this view and draft. Your saved conversation remains in history.",
  "chat.reset.cancel": "Keep this conversation",
  // De knop op de hoek van een vraagbubbel draagt geen woord; haar naam noemt
  // daarom de vraag die meegaat.
  "chat.compare.action.question": "Ask the other pigeon “{question}”",
  "chat.compare.busy": "Taking your question to the other pigeon…",
  "chat.compare.origin": "Continued from your conversation with {pigeon}",
  // De herhaalde vraag staat als volle bubbel in de opslag, maar leest op het
  // scherm als één regel: het gesprek is doorgegeven, de lezer vraagt niets
  // opnieuw.
  "chat.compare.repeated": "The same question, now to {pigeon}",
  "chat.compare.back": "Back to that conversation",
  "chat.delete.action": "Delete",
  "chat.delete.title": "Delete this saved conversation?",
  "chat.delete.body": "This removes its messages and sources from your account. Whatever travelled to another pigeon — your questions and this pigeon’s answers with their sources — stays in those conversations; only the link back to this original disappears. This cannot be undone.",
  "chat.delete.cancel": "Keep conversation",
  "chat.delete.busy": "Deleting…",
  "chat.delete.failed": "Deletion could not be confirmed. Try again; the conversation stays visible until confirmed.",
  "chat.author.unknown": "Earlier pigeon (identity unavailable)",
  "chat.guidance": "Facilitator",
  "chat.progress": "Looking into your question…",
  // The steps of one turn. A chat turn is four or five model calls in
  // a row; without these the reader sees half a minute of "thinking" and cannot
  // tell a slow answer from a stalled one. Each label is what is happening now,
  // in the reader's own words: no tool names, no phases, no machine talk.
  "chat.step.facilitator": "Working out the question",
  "chat.step.pigeon_grounding": "Reading sources",
  // Deliberately its own step: after a knowledge gap the collection is searched
  // once more, widely, for research that borders on the question.
  // That costs seconds, and the reader is owed the reason for them.
  "chat.step.pigeon_related_search": "Looking for related sources",
  "chat.step.pigeon": "Writing the answer",
  "chat.step.mediation": "The facilitator is wrapping up",
  // De stapregel zelf, voor de schermlezer: wat er nu gebeurt, één keer per
  // wissel. De chips eronder zijn dezelfde tekst en worden niet los gemeld.
  "chat.steps.label": "Progress of this answer",
  "chat.steps.busy": "Now: {step}",
  "chat.noAnswer": "No answer was completed. You can rephrase your question or start a new attempt.",
  // De laatste vraag kreeg geen antwoord en er loopt niets meer:
  // meestal sloot het tabblad terwijl de beurt liep.
  "chat.interruptedTurn": "This turn stopped before an answer came. You can send the question again.",
  "chat.version": "About this answer",
  "chat.version.legacy": "This older answer has no recorded version details. Its original text and sources are shown.",
  "chat.version.model": "Model",
  "chat.version.contract": "Behaviour contract",
  "chat.version.persona": "Pigeon version",
  "chat.version.prompt": "Instruction version",
  "chat.version.kb": "Research collection version",
  "chat.version.unverified": "Not verified for this answer",
  "chat.source.unavailable": "Sources are temporarily unavailable. Try again later.",
  "chat.language.notice": "This conversation continues in {language}. Start a new conversation to use the current interface language.",
  "chat.language.en": "English",
  "chat.language.nl": "Dutch",
  "chat.retry": "Edit the last question",
  'chat.new': 'New conversation',
  'chat.sessions.title': 'Conversations',
  'chat.sessions.empty': 'No conversations yet. Ask your first question.',
  'chat.sessions.open': 'Open the conversation with {pigeon}',
  'chat.placeholder': 'Ask the pigeon…',
  'chat.thinking': 'The pigeon is thinking…',
  // Twee stemmen, twee spinners: de facilitator denkt niet namens de duif.
  'chat.thinking.facilitator': 'The facilitator is thinking…',
  'chat.empty': 'Ask a question to start the conversation.',
  'chat.citation': 'Source {number}',
  'chat.source.count.one': '1 source',
  'chat.source.count.many': '{count} sources',
  // These sources come from the broader search that runs after the
  // normal source threshold finds nothing. "Related" says where they came
  // from, not how much they are worth: sometimes they carry the answer, and
  // sometimes they only touch its edges. The badge therefore states the
  // relation and leaves the verdict to the pigeon's own words — a badge that
  // claims there is no source contradicts every answer that does cite one.
  'chat.source.related.one': '1 related source',
  'chat.source.related.many': '{count} related sources',
  'chat.source.related': 'These sources border on your question.',
  'chat.source.panel': 'Source',
  'chat.source.position': 'Source {number} of {total}',
  'chat.source.close': 'Close the source',
  'chat.source.previous': 'Previous source',
  'chat.source.next': 'Next source',
  'chat.source.trail': 'Place in the research collection',
  'chat.source.about': 'About this source',
  'chat.gap': 'No source found for this question.',
  // Zonder bron mag de Stakeholder een begrensd standpunt innemen. Het
  // label zegt wat er dan staat, in gewone taal en zonder verwijt.
  'chat.stance.withoutResearch': 'Position without research',
  // Wat er wegviel vóór de bronnenlijst. Eén neutrale zin voor de
  // lezer: geen foutcode, geen techniek, geen schuldige. De redenen
  // eronder staan alleen in devMode, voor de begeleider op de stand.
  'chat.dropped.one': 'One source that was found did not make it into this answer.',
  'chat.dropped.many': '{count} sources that were found did not make it into this answer.',
  'chat.dropped.detail': 'What fell away',
  'chat.dropped.no_text': 'no readable text ({count})',
  'chat.dropped.other_knowledge_base': 'from an earlier knowledge base ({count})',
  'chat.dropped.evidence_unreadable': 'evidence could not be read ({count})',
  'chat.dropped.partial_search': 'part of the search failed ({count})',
  'chat.source.citation': 'Reference:',
  'chat.source.citationMissing': 'Bibliographic details to check: {missing}. This is not an assessment of the research quality.',
  'chat.source.paper': 'From {paper}',
  'chat.source.key': 'Source key: {key}',
  'chat.source.doi': 'doi: {doi}',
  'chat.source.graph': 'Open in the knowledge graph',
  'chat.interrupted': '(interrupted)',
  'chat.dismiss': 'Dismiss',
  'chat.latest': 'Jump to latest',
  // Alleen de stopredenen die een lezer iets vertellen. Een interne reden
  // (te veel stappen, een mislukte stap) zegt niets over het antwoord en
  // staat er dus niet meer; de onderbroken-markering en de foutbanner doen
  // dat werk al.
  //
  // "truncated" noemt ook de oorzaak: de zin houdt op omdat de beurt tegen
  // haar lengtegrens liep, niet omdat de duif uitgesproken was. Zonder die
  // toevoeging leest een halve zin als een eigenaardige stijlkeuze.
  //
  // "filtered" zegt wie er stopte, niet wat de lezer ziet. De oude tekst
  // ("dit antwoord is niet getoond") kwam uit de tijd dat deze reden alleen
  // van de agent-lus kwam, die afbrak vóór er tekst stond. Sinds de
  // duif-stream hem ook kan melden, kan er wél een halve alinea staan — en dan
  // sprak die tekst het scherm tegen.
  'chat.stopped.refusal': 'The pigeon did not answer this question.',
  'chat.stopped.truncated': 'This answer ran into the length limit and stops halfway.',
  'chat.stopped.filtered': 'The model’s own filter stopped this answer.',
  'chat.announce.pigeon': 'The pigeon has answered.',
  'chat.announce.facilitator': 'The facilitator has answered.',
} as const

export const chatNl: Record<keyof typeof chatEn, string> = {
  'chat.source.perspective': 'Perspectief',
  'chat.source.finding': 'Het standpunt uit deze bron',
  'chat.source.summary': 'Zo is deze bron samengevat; lees de oorspronkelijke passage hieronder om dat te controleren.',
  'chat.source.summaryAlone': 'Zo is deze bron samengevat; de oorspronkelijke passage staat er hier niet bij om dat aan te controleren.',
  'chat.source.original': 'De originele woorden uit de paper',
  'chat.source.originalPlain': 'De originele woorden',
  'chat.source.unverified': 'Deze samenvatting is niet met de oorspronkelijke tekst vergeleken.',
  'chat.source.rejected': 'De oorspronkelijke passages van deze bron waren niet in de paper terug te vinden en staan er daarom niet bij; de samenvatting is ongecontroleerd.',
  'chat.source.translated': 'Vertaalde samenvatting; het citaat is het origineel.',
  "chat.history.toggle": "Historie",
  "chat.history.refresh": "Geschiedenis vernieuwen",
  "chat.history.refreshing": "Geschiedenis vernieuwen…",
  "chat.history.failed": "De geschiedenis kon niet worden vernieuwd. Je huidige gesprek is behouden. Probeer opnieuw te vernieuwen.",
  "chat.history.scope": "Alleen gesprekken met deze duif",
  "chat.history.comparison": "Vergelijking",
  "chat.history.loading": "Je gesprek wordt geladen…",
  "chat.starters.title": "Begin met een vraag",
  "chat.starters.intro": "Kies een voorbeeld en maak het je eigen. Jij bepaalt wanneer je het verstuurt.",
  "chat.starters.question": "Vraag",
  "chat.starters.case": "Casus om te bespreken",
  "chat.draft.title": "Je concept behouden?",
  "chat.draft.body": "Er staat al tekst in je invoer. Kies of je die wilt houden of vervangen door dit voorbeeld.",
  "chat.draft.keep": "Behoud mijn concept",
  "chat.draft.replace": "Gebruik dit voorbeeld",
  "chat.reset.title": "Met een leeg gesprek beginnen?",
  "chat.reset.body": "Dit stopt het huidige antwoord en maakt dit scherm en concept leeg. Je opgeslagen gesprek blijft in de historie.",
  "chat.reset.cancel": "Behoud dit gesprek",
  "chat.compare.action.question": "Vraag “{question}” ook aan de andere duif",
  "chat.compare.busy": "Je vraag gaat naar de andere duif…",
  "chat.compare.origin": "Voortgezet uit je gesprek met {pigeon}",
  "chat.compare.repeated": "Dezelfde vraag, nu aan {pigeon}",
  "chat.compare.back": "Terug naar dat gesprek",
  "chat.delete.action": "Verwijderen",
  "chat.delete.title": "Dit opgeslagen gesprek verwijderen?",
  "chat.delete.body": "Dit verwijdert de berichten en bronnen uit je account. Wat naar een andere duif meereisde — jouw vragen en de antwoorden van deze duif met hun bronnen — blijft in die gesprekken staan; alleen de verwijzing naar dit origineel verdwijnt. Dit kun je niet terugdraaien.",
  "chat.delete.cancel": "Gesprek bewaren",
  "chat.delete.busy": "Verwijderen…",
  "chat.delete.failed": "Verwijderen kon niet bevestigd worden. Probeer het opnieuw; het gesprek blijft zichtbaar totdat dit bevestigd is.",
  "chat.author.unknown": "Eerdere duif (identiteit onbekend)",
  "chat.guidance": "Facilitator",
  "chat.progress": "Je vraag wordt onderzocht…",
  "chat.step.facilitator": "Vraag formuleren",
  "chat.step.pigeon_grounding": "Bronnen lezen",
  "chat.step.pigeon_related_search": "Verwante bronnen zoeken",
  "chat.step.pigeon": "Antwoord schrijven",
  "chat.step.mediation": "Facilitator rondt af",
  "chat.steps.label": "Voortgang van dit antwoord",
  "chat.steps.busy": "Nu bezig: {step}",
  "chat.noAnswer": "Er is geen antwoord afgerond. Je kunt je vraag aanpassen of een nieuwe poging beginnen.",
  "chat.interruptedTurn": "Deze beurt is afgebroken voordat er een antwoord kwam. Je kunt de vraag opnieuw versturen.",
  "chat.version": "Over dit antwoord",
  "chat.version.legacy": "Bij dit oudere antwoord zijn geen versiegegevens vastgelegd. De oorspronkelijke tekst en bronnen worden getoond.",
  "chat.version.model": "Model",
  "chat.version.contract": "Gedragscontract",
  "chat.version.persona": "Duifversie",
  "chat.version.prompt": "Instructieversie",
  "chat.version.kb": "Versie onderzoekscollectie",
  "chat.version.unverified": "Niet geverifieerd voor dit antwoord",
  "chat.source.unavailable": "De bronnen zijn tijdelijk niet beschikbaar. Probeer het later opnieuw.",
  "chat.language.notice": "Dit gesprek gaat verder in het {language}. Start een nieuw gesprek om de huidige interfacetaal te gebruiken.",
  "chat.language.en": "Engels",
  "chat.language.nl": "Nederlands",
  "chat.retry": "Bewerk de laatste vraag",
  'chat.new': 'Nieuw gesprek',
  'chat.sessions.title': 'Gesprekken',
  'chat.sessions.empty': 'Nog geen gesprekken. Stel je eerste vraag.',
  'chat.sessions.open': 'Open het gesprek met {pigeon}',
  'chat.placeholder': 'Vraag het de duif…',
  'chat.thinking': 'De duif denkt na…',
  'chat.thinking.facilitator': 'De facilitator denkt na…',
  'chat.empty': 'Stel een vraag om het gesprek te beginnen.',
  'chat.citation': 'Bron {number}',
  'chat.source.count.one': '1 bron',
  'chat.source.count.many': '{count} bronnen',
  'chat.source.related.one': '1 verwante bron',
  'chat.source.related.many': '{count} verwante bronnen',
  'chat.source.related': 'Deze bronnen grenzen aan je vraag.',
  'chat.source.panel': 'Bron',
  'chat.source.position': 'Bron {number} van {total}',
  'chat.source.close': 'De bron sluiten',
  'chat.source.previous': 'Vorige bron',
  'chat.source.next': 'Volgende bron',
  'chat.source.trail': 'Plek in de onderzoekscollectie',
  'chat.source.about': 'Over deze bron',
  'chat.gap': 'Geen bron gevonden voor deze vraag.',
  'chat.stance.withoutResearch': 'Standpunt zonder onderzoek',
  'chat.dropped.one': 'Eén gevonden bron viel af en staat niet bij dit antwoord.',
  'chat.dropped.many': '{count} gevonden bronnen vielen af en staan niet bij dit antwoord.',
  'chat.dropped.detail': 'Wat er afviel',
  'chat.dropped.no_text': 'geen leesbare tekst ({count})',
  'chat.dropped.other_knowledge_base': 'uit een eerdere kennisbank ({count})',
  'chat.dropped.evidence_unreadable': 'bewijs niet te lezen ({count})',
  'chat.dropped.partial_search': 'een deel van de zoekopdracht faalde ({count})',
  'chat.source.citation': 'Bronvermelding:',
  'chat.source.citationMissing': 'Te controleren bibliografische gegevens: {missing}. Dit is geen oordeel over de kwaliteit van het onderzoek.',
  'chat.source.paper': 'Uit {paper}',
  'chat.source.key': 'Bronsleutel: {key}',
  'chat.source.doi': 'doi: {doi}',
  'chat.source.graph': 'Open in de kennisgraaf',
  'chat.interrupted': '(afgebroken)',
  'chat.dismiss': 'Sluiten',
  'chat.latest': 'Naar het laatste bericht',
  'chat.stopped.refusal': 'De duif heeft deze vraag niet beantwoord.',
  'chat.stopped.truncated': 'Dit antwoord liep tegen de lengtegrens aan en stopt halverwege.',
  'chat.stopped.filtered': 'Het filter van het model heeft dit antwoord gestopt.',
  'chat.announce.pigeon': 'De duif heeft geantwoord.',
  'chat.announce.facilitator': 'De facilitator heeft geantwoord.',
}
