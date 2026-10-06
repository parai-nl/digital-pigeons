// Teksten van de vergaderweergave. Eigenaar: lib/meeting. Zelfde typeregel als
// common.ts: nl is getypeerd op en, dus svelte-check keurt een ontbrekende of
// overbodige sleutel af.
export const meetingEn = {
  "meeting.module.loading": "Opening meeting setup…",
  "meeting.module.failed": "The meeting screen could not be opened. You can retry; this does not start the microphone.",
  "meeting.answer.unavailable": "The knowledge base could not be reached for this contribution.",

  "meeting.setup.eyebrow": "Meeting",
  "meeting.setup.speaking": "A pigeon perspective joins your conversation. Give it a question or let it contribute when relevant.",
  "meeting.setup.signal": "Movement and sound join the conversation. The signal has no assigned translation or meaning.",
  "meeting.setup.language": "Meeting language",
  // De hint zegt per duif wat de standaardstand is en waarom: de
  // signalerende duif doet vanzelf mee, de pratende twee wachten op een
  // uitnodiging. De schakelaar zelf blijft per vergadering te verzetten.
  "meeting.setup.autonomousHint.signal": "On by default. This pigeon has no words: she answers the conversation with sound and movement, and not after every sentence. Switch it off and she only reacts when you invite her.",
  "meeting.setup.autonomousHint.speaking": "Off by default. A spoken contribution interrupts the conversation, so you invite it yourself. Switch it on and she steps in when the conversation concerns her.",
  "meeting.setup.data": "Starting opens the microphone and sends speech for processing. Meeting text stays in temporary session memory and clears when you end.",
  "meeting.check.action": "Check microphone & browser audio",
  "meeting.check.checking": "Speak briefly. This local check closes the microphone after three seconds; it sends no audio.",
  "meeting.check.ok": "Microphone and sound both work. The microphone is closed again.",
  "meeting.check.noInput": "No sound reached the microphone. Check which microphone the browser is using.",
  "meeting.check.noSound": "The microphone works, but the browser is still holding sound back.",
  "meeting.check.refused": "Microphone permission was refused. Allow access in the browser and try again.",
  "meeting.check.unavailable": "The browser could not open audio. Check the browser and device settings.",
  "meeting.retry": "Try again",
  "meeting.cancelStart": "Cancel start",
  "meeting.permission": "Waiting for microphone permission…",
  "meeting.settings.advanced": "Advanced meeting settings",
  "meeting.stage.ready": "At the table",
  "meeting.stage.content": "Pigeon contribution and meeting context",
  "meeting.stage.invitation": "What would you like to discuss together?",
  "meeting.stage.hint": "Start the conversation and invite the pigeon when you want a contribution. Its latest response stays here.",
  "meeting.stage.listening": "The conversation is under way. Invite the pigeon when you want a contribution; its response appears here.",
  "meeting.answer.writing": "Preparing a contribution",
  // De stem loopt achter op de tekst: het geschreven antwoord staat er al
  // compleet terwijl de duif het nog uitspreekt. Dat is "spreekt", niet
  // "voorbereiden".
  "meeting.answer.speaking": "Speaking",
  "meeting.answer.preparing": "One moment…",
  "meeting.answer.stopped": "This contribution was stopped; it may be incomplete.",
  "meeting.answer.interrupted": "This contribution was interrupted; it may be incomplete.",
  "meeting.answer.gap": "The available sources did not provide a direct answer to this question.",
  // Wat er wegviel vóór de bronnenlijst. Eén neutrale zin voor de
  // zaal; de redenen erachter staan alleen in devMode, voor de begeleider.
  "meeting.dropped.one": "One source that was found did not make it into this contribution.",
  "meeting.dropped.many": "{count} sources that were found did not make it into this contribution.",
  "meeting.preview.final": "Recent speech",
  "meeting.preview.partial": "Speech being recognised",
  "meeting.signal.audioBlocked": "The browser is blocking sound. The visual signal remains available.",
  "meeting.signal.audioFailed": "The sound could not play. The visual signal remains available.",
  // De staat van de duif in één woord, naast haar naam op het podium: zij doet
  // even niet mee. De microfoonstand staat er apart naast en zegt iets anders.
  "meeting.state.quiet": "Quiet",
  "meeting.resume": "Resume contribution",
  "meeting.resume.hint": "Lets the pigeon join in on its own again. The microphone has its own control.",

  // --- opzet ---------------------------------------------------------------
  'meeting.setup.title': 'Seat a pigeon at the table',
  'meeting.setup.intro': 'Choose a place to discuss, check the room and start when everyone is ready.',
  'meeting.setup.pigeon': 'Pigeon',
  'meeting.setup.autonomous': 'Let the pigeon step in on its own',
  'meeting.setup.eagerness': 'Eagerness',
  'meeting.setup.cooldown': 'Minimum time between interventions',
  'meeting.setup.cooldown.own': "the pigeon's own rhythm",
  'meeting.setup.cooldown.max': '1 hour',
  'meeting.setup.seconds': '{n} s',
  'meeting.seat': 'Seat the pigeon',
  'meeting.seating': 'Seating the pigeon…',
  'meeting.start.failed': 'Could not seat the pigeon at the table.',
  'meeting.eagerness.reserved': 'Reserved',
  'meeting.eagerness.normal': 'Normal',
  'meeting.eagerness.eager': 'Eager',

  // --- vergaderstand: vier woorden, plus aandacht ---------------------------
  'meeting.status.attention': 'Something needs attention',
  'meeting.status.speaking': 'The pigeon is speaking',
  'meeting.status.thinking': 'The pigeon is thinking',
  'meeting.status.listening': 'The pigeon is listening',
  'meeting.status.paused': 'Paused',

  // --- luisterindicator ------------------------------------------------
  'meeting.listen.listening': 'The pigeon is listening',
  'meeting.listen.reconnecting': 'Reconnecting…',
  'meeting.listen.off': 'The pigeon is not listening',
  'meeting.listen.muted': 'The pigeon is not listening — microphone muted',

  // --- bediening ------------------------------------------------------------
  'meeting.respond': 'Let the pigeon respond',
  'meeting.intervene': 'Let the pigeon step in',
  'meeting.stop': 'Stop contribution',
  'meeting.stop.hint': 'Silences the pigeon at once and stops it joining in on its own. The meeting and the microphone keep running.',
  'meeting.end': 'End meeting',
  // Beëindigen is onomkeerbaar en wist de vergadertekst. Staat de duif nog te
  // spreken, dan is de klik waarschijnlijk bedoeld om háár stil te krijgen;
  // daarom die ene vraag, met de stilteknop als uitweg.
  'meeting.end.title': 'End the meeting?',
  'meeting.end.body': 'The pigeon is still speaking. Ending closes the session and the microphone, and clears the meeting text from this screen. To silence only the pigeon and keep the meeting running, choose Stop contribution.',
  'meeting.end.confirm': 'Yes, end the meeting',
  'meeting.end.cancel': 'No, keep the meeting running',
  'meeting.mute': 'Mute microphone',
  'meeting.unmute': 'Unmute microphone',

  // --- meldingen ------------------------------------------------------------
  'meeting.mic.none': 'No microphone available: the pigeon cannot listen along. The button still works.',
  'meeting.mic.refused': 'Microphone access was refused: the pigeon cannot listen along. The button still works.',
  'meeting.voice.off': 'Text without voice: this pigeon has no voice configured. It answers on screen.',
  'meeting.audio.blocked': 'The browser is holding back sound. The pigeon answers in text until you allow it.',
  'meeting.audio.enable': 'Turn sound on',
  'meeting.notice.sound': 'The sound did not play in full. Read the contribution on screen; it may be incomplete.',
  'meeting.notice.voiceUnavailable': 'The voice is not available right now. The text is on screen.',
  'meeting.notice.stopFailed': 'Stop contribution worked on this screen, but the meeting server did not confirm it. Press Stop contribution again if the pigeon keeps going.',
  'meeting.detector.paused': 'Automatic participation is unavailable. You can try a manual invitation.',

  // Het kandidaatblok: de duif heeft iets klaarliggen. Wat zij wil zeggen
  // staat er niet — dat hoor je pas als het klinkt. Zij wacht niet op een
  // stilte: zij breekt zelf in, en de knop is de snelweg. Alleen
  // wie haar aanspreekt krijgt haar antwoord ná de lopende zin.
  'meeting.candidate.topic': 'The pigeon has something to say on this theme:',
  'meeting.candidate.general': 'The pigeon has something to say about this',
  'meeting.candidate.addressed': 'The pigeon has an answer for you',
  'meeting.candidate.signal': 'The pigeon wants to make a sound',
  'meeting.candidate.breaking-in': 'It breaks in by itself; this button is the shortcut.',
  'meeting.candidate.after-the-sentence': 'It answers as soon as the sentence is finished.',
  'meeting.candidate.sources.one': '1 source',
  'meeting.candidate.sources.many': '{n} sources',
  'meeting.candidate.speak': 'Let it be heard',
  'meeting.closed': 'The meeting session was closed',
  'meeting.closed.inactivity': 'nobody answered “Are you still there?”',
  'meeting.closed.ended_by_user': 'you ended it',
  'meeting.closed.max_duration': 'the maximum meeting length was reached',
  'meeting.closed.server_shutdown': 'the server stopped',
  'meeting.closed.connection_lost': 'the connection was lost',
  'meeting.closed.replaced_by_user': 'you continued in another meeting',
  'meeting.closed.client_gone': 'this screen was gone for too long',
  'meeting.closed.unknown': 'the reason is unknown',

  // --- vastzitten door een eigen open vergadering ----------------------
  'meeting.stuck.action': 'End the other meeting and continue here',
  'meeting.stuck.title': 'End the other meeting?',
  'meeting.stuck.body': 'The meeting that is still open on this account ends now. Its transcript disappears, as it always does when a meeting ends. Then this pigeon takes its place at the table.',
  'meeting.stuck.confirm': 'End it and continue here',
  'meeting.stuck.cancel': 'Leave it open',
  'meeting.stuck.busy': 'Ending the other meeting…',

  // --- een andere duif aan tafel --------------------------------------
  'meeting.switch.title': 'Seat a different pigeon at the table?',
  'meeting.switch.body': 'The meeting continues: the microphone stays on and what has been said so far goes along, so {pigeon} can respond to it. The contributions of {previous} stay hers.',
  'meeting.switch.confirm': 'Yes, seat {pigeon}',
  'meeting.switch.cancel': 'No, stay with {previous}',
  'meeting.switch.failed': 'Could not seat the other pigeon at the table. The meeting continues unchanged.',
  'meeting.switch.carried': 'The last contribution from {previous}, before {pigeon} took her place at the table.',

  // --- transcript -----------------------------------------------------------
  'meeting.transcript.title': 'Transcript',
  'meeting.transcript.notice': 'Temporary session memory — up to 80 recent lines. Ending the meeting clears them.',
  'meeting.transcript.empty': 'Nothing said yet.',
  'meeting.transcript.expand': 'Full transcript',
  'meeting.transcript.own': 'During pigeon audio',
  'meeting.speaker': 'Speaker {n}',

  // --- aanwezigheid ----------------------------------------------------
  // De uitleg is dezelfde als die van de server (presenceCheckMessage); deze
  // staat er voor het geval de server er geen meegeeft. Hij noemt geen
  // minuten: die staan in de config. De knop om te beëindigen is
  // 'meeting.end', dezelfde woorden als de rode knop, want het is dezelfde
  // handeling.
  'meeting.presence.title': 'Are you still there?',
  'meeting.presence.body': 'Nothing has been clicked or typed on this screen for a while. Without an answer the meeting stops by itself: the microphone closes and the meeting text disappears.',
  'meeting.presence.confirm': 'Yes, carry on',
  'meeting.presence.countdown': 'The meeting stops in {time}',
  'meeting.presence.soon': 'The meeting stops in 30 seconds unless someone answers.',

  // --- instellingen ----------------------------------------------------
  'meeting.answers.reconnected': '(reconnected)',
} as const

export const meetingNl: Record<keyof typeof meetingEn, string> = {
  "meeting.module.loading": "Vergaderopzet openen…",
  "meeting.module.failed": "Het vergaderscherm kon niet worden geopend. Je kunt opnieuw proberen; dit start de microfoon niet.",
  "meeting.answer.unavailable": "De kennisbank kon voor deze bijdrage niet worden bereikt.",

  "meeting.setup.eyebrow": "Vergadering",
  "meeting.setup.speaking": "Een duivenperspectief schuift aan bij jullie gesprek. Geef de duif een vraag of laat haar bijdragen wanneer dat relevant is.",
  "meeting.setup.signal": "Beweging en geluid doen mee aan het gesprek. Het signaal heeft geen vastgelegde vertaling of betekenis.",
  "meeting.setup.language": "Vergadertaal",
  "meeting.setup.autonomousHint.signal": "Standaard aan. Deze duif heeft geen woorden: zij reageert met geluid en beweging op het gesprek, en niet na elke zin. Zet je hem uit, dan reageert zij alleen als je haar uitnodigt.",
  "meeting.setup.autonomousHint.speaking": "Standaard uit. Een gesproken bijdrage onderbreekt het gesprek, dus je nodigt haar zelf uit. Zet je hem aan, dan valt zij in wanneer het gesprek haar aangaat.",
  "meeting.setup.data": "Beginnen opent de microfoon en verstuurt spraak voor verwerking. Vergadertekst blijft in tijdelijk sessiegeheugen en verdwijnt wanneer je beëindigt.",
  "meeting.check.action": "Controleer microfoon & browseraudio",
  "meeting.check.checking": "Zeg kort iets. Deze lokale controle sluit de microfoon na drie seconden; ze verstuurt geen audio.",
  "meeting.check.ok": "Microfoon en geluid werken allebei. De microfoon is weer gesloten.",
  "meeting.check.noInput": "Er kwam geen geluid bij de microfoon binnen. Kijk welke microfoon de browser gebruikt.",
  "meeting.check.noSound": "De microfoon werkt, maar de browser houdt geluid nog tegen.",
  "meeting.check.refused": "Microfoontoegang is geweigerd. Geef toestemming in de browser en probeer opnieuw.",
  "meeting.check.unavailable": "De browser kon audio niet openen. Controleer de browser- en apparaatinstellingen.",
  "meeting.retry": "Probeer opnieuw",
  "meeting.cancelStart": "Start annuleren",
  "meeting.permission": "Wachten op toestemming voor de microfoon…",
  "meeting.settings.advanced": "Geavanceerde vergaderinstellingen",
  "meeting.stage.ready": "Aan tafel",
  "meeting.stage.content": "Duifbijdrage en vergadercontext",
  "meeting.stage.invitation": "Wat willen jullie samen bespreken?",
  "meeting.stage.hint": "Begin het gesprek en nodig de duif uit wanneer je een bijdrage wilt. Het laatste antwoord blijft hier staan.",
  "meeting.stage.listening": "Het gesprek loopt. Nodig de duif uit wanneer je een bijdrage wilt; haar antwoord komt hier te staan.",
  "meeting.answer.writing": "Bijdrage voorbereiden",
  "meeting.answer.speaking": "Spreekt",
  "meeting.answer.preparing": "Een moment…",
  "meeting.answer.stopped": "Deze bijdrage is gestopt en kan onvolledig zijn.",
  "meeting.answer.interrupted": "Deze bijdrage is onderbroken en kan onvolledig zijn.",
  "meeting.answer.gap": "De beschikbare bronnen gaven geen direct antwoord op deze vraag.",
  "meeting.dropped.one": "Eén gevonden bron viel af en staat niet bij deze bijdrage.",
  "meeting.dropped.many": "{count} gevonden bronnen vielen af en staan niet bij deze bijdrage.",
  "meeting.preview.final": "Recente spraak",
  "meeting.preview.partial": "Spraak wordt herkend",
  "meeting.signal.audioBlocked": "De browser blokkeert geluid. Het zichtbare signaal blijft beschikbaar.",
  "meeting.signal.audioFailed": "Het geluid kon niet afspelen. Het zichtbare signaal blijft beschikbaar.",
  "meeting.state.quiet": "Stil",
  "meeting.resume": "Inbreng hervatten",
  "meeting.resume.hint": "Laat de duif weer uit zichzelf meedoen. De microfoon heeft een eigen bediening.",

  'meeting.setup.title': 'Zet een duif aan tafel',
  'meeting.setup.intro': 'Kies een plek om te bespreken, controleer de ruimte en begin wanneer iedereen klaar is.',
  'meeting.setup.pigeon': 'Duif',
  'meeting.setup.autonomous': 'Laat de duif zelf ingrijpen',
  'meeting.setup.eagerness': 'Gretigheid',
  'meeting.setup.cooldown': 'Minimale tijd tussen interventies',
  'meeting.setup.cooldown.own': 'eigen ritme van de duif',
  'meeting.setup.cooldown.max': '1 uur',
  'meeting.setup.seconds': '{n} s',
  'meeting.seat': 'Zet de duif aan tafel',
  'meeting.seating': 'De duif gaat zitten…',
  'meeting.start.failed': 'Kon de duif niet aan tafel zetten.',
  'meeting.eagerness.reserved': 'Terughoudend',
  'meeting.eagerness.normal': 'Normaal',
  'meeting.eagerness.eager': 'Gretig',

  'meeting.status.attention': 'Er is iets aan de hand',
  'meeting.status.speaking': 'De duif spreekt',
  'meeting.status.thinking': 'De duif denkt na',
  'meeting.status.listening': 'De duif luistert',
  'meeting.status.paused': 'Gepauzeerd',

  'meeting.listen.listening': 'De duif luistert mee',
  'meeting.listen.reconnecting': 'Verbinding herstellen…',
  'meeting.listen.off': 'De duif luistert niet',
  'meeting.listen.muted': 'De duif luistert niet — microfoon gedempt',

  'meeting.respond': 'Laat de duif antwoorden',
  'meeting.intervene': 'Laat de duif ingrijpen',
  'meeting.stop': 'Stop inbreng',
  'meeting.stop.hint': 'Zet de duif meteen stil en laat haar niet meer uit zichzelf meedoen. De vergadering en de microfoon lopen door.',
  'meeting.end': 'Vergadering beëindigen',
  'meeting.end.title': 'De vergadering beëindigen?',
  'meeting.end.body': 'De duif is nog aan het spreken. Beëindigen sluit de sessie en de microfoon, en wist de vergadertekst uit dit scherm. Wil je alleen de duif stil hebben en de vergadering laten lopen, kies dan Stop inbreng.',
  'meeting.end.confirm': 'Ja, beëindig de vergadering',
  'meeting.end.cancel': 'Nee, laat de vergadering lopen',
  'meeting.mute': 'Microfoon dempen',
  'meeting.unmute': 'Microfoon aanzetten',

  'meeting.mic.none': 'Geen microfoon beschikbaar: de duif kan niet meeluisteren. De knop werkt nog.',
  'meeting.mic.refused': 'Toegang tot de microfoon is geweigerd: de duif kan niet meeluisteren. De knop werkt nog.',
  'meeting.voice.off': 'Tekst zonder stem: deze duif heeft geen stem ingesteld. Ze antwoordt op het scherm.',
  'meeting.audio.blocked': 'De browser houdt geluid tegen. De duif antwoordt in tekst tot je het toestaat.',
  'meeting.audio.enable': 'Geluid aanzetten',
  'meeting.notice.sound': 'Het geluid speelde niet volledig af. Lees de bijdrage op het scherm; die kan onvolledig zijn.',
  'meeting.notice.voiceUnavailable': 'De stem is nu niet beschikbaar. De tekst staat wel op het scherm.',
  'meeting.notice.stopFailed': 'Stop inbreng werkte op dit scherm, maar de server bevestigde het niet. Druk nog een keer op Stop inbreng als de duif toch doorgaat.',
  'meeting.detector.paused': 'Automatisch deelnemen is niet beschikbaar. Je kunt een handmatige uitnodiging proberen.',

  'meeting.candidate.topic': 'De duif heeft iets te zeggen over dit thema:',
  'meeting.candidate.general': 'De duif heeft hier iets over te zeggen',
  'meeting.candidate.addressed': 'De duif heeft een antwoord voor je',
  'meeting.candidate.signal': 'De duif wil een geluid geven',
  'meeting.candidate.breaking-in': 'Ze breekt zelf in; deze knop is de snelweg.',
  'meeting.candidate.after-the-sentence': 'Ze antwoordt zodra de zin af is.',
  'meeting.candidate.sources.one': '1 bron',
  'meeting.candidate.sources.many': '{n} bronnen',
  'meeting.candidate.speak': 'Laat horen',
  'meeting.closed': 'De vergadersessie is gesloten',
  'meeting.closed.inactivity': 'niemand antwoordde op “Ben je er nog?”',
  'meeting.closed.ended_by_user': 'je hebt hem beëindigd',
  'meeting.closed.max_duration': 'de maximale vergaderduur is bereikt',
  'meeting.closed.server_shutdown': 'de server is gestopt',
  'meeting.closed.connection_lost': 'de verbinding is verbroken',
  'meeting.closed.replaced_by_user': 'je bent in een andere vergadering verdergegaan',
  'meeting.closed.client_gone': 'dit scherm was te lang weg',
  'meeting.closed.unknown': 'de reden is onbekend',

  'meeting.stuck.action': 'Beëindig de andere vergadering en ga hier verder',
  'meeting.stuck.title': 'De andere vergadering beëindigen?',
  'meeting.stuck.body': 'De vergadering die op dit account nog openstaat, wordt nu beëindigd. Het transcript daarvan verdwijnt, zoals altijd bij het einde van een vergadering. Daarna neemt deze duif de plaats aan tafel in.',
  'meeting.stuck.confirm': 'Beëindig hem en ga hier verder',
  'meeting.stuck.cancel': 'Laat hem openstaan',
  'meeting.stuck.busy': 'De andere vergadering beëindigen…',

  'meeting.switch.title': 'Wil je een andere duif aan tafel zetten?',
  'meeting.switch.body': 'De vergadering loopt door: de microfoon blijft aan en wat er tot nu toe gezegd is gaat mee, zodat {pigeon} daarop kan reageren. De bijdragen van {previous} blijven van haar.',
  'meeting.switch.confirm': 'Ja, zet {pigeon} aan tafel',
  'meeting.switch.cancel': 'Nee, blijf bij {previous}',
  'meeting.switch.failed': 'Kon de andere duif niet aan tafel zetten. De vergadering loopt onveranderd door.',
  'meeting.switch.carried': 'De laatste bijdrage van {previous}, voordat {pigeon} aan tafel ging.',


  'meeting.transcript.title': 'Transcript',
  'meeting.transcript.notice': 'Tijdelijk sessiegeheugen — maximaal 80 recente regels. Beëindigen wist deze tekst.',
  'meeting.transcript.empty': 'Er is nog niets gezegd.',
  'meeting.transcript.expand': 'Volledig transcript',
  'meeting.transcript.own': 'Tijdens duivenaudio',
  'meeting.speaker': 'Spreker {n}',

  'meeting.presence.title': 'Ben je er nog?',
  'meeting.presence.body': 'Er is al een tijdje niets aangeklikt of getypt op dit scherm. Zonder antwoord stopt de vergadering vanzelf: de microfoon gaat dicht en de vergadertekst verdwijnt.',
  'meeting.presence.confirm': 'Ja, ga door',
  'meeting.presence.countdown': 'De vergadering stopt over {time}',
  'meeting.presence.soon': 'De vergadering stopt over 30 seconden als niemand antwoordt.',

  'meeting.answers.reconnected': '(opnieuw verbonden)',
}
