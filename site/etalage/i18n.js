// Alle tekst van de etalage, Nederlands en Engels in pariteit. De toon: kort,
// concreet, speels waar het kan. scripts/check-etalage.mjs bewaakt dat beide
// talen dezelfde sleutels hebben.
//
// Hier staat geen duiventekst. Wat een duif zegt, komt uit de opnames
// (data/opnames.json); de teksten hieronder zijn de pagina zelf.
//
// Een waarde die van de data afhangt, is een functie. Die krijgt de feiten al
// opgemaakt in de taal van de pagina (f.publications = "118", f.claims = "4.067") en
// laat een zin weg zolang een getal er niet is: liever een zin minder dan een
// getal dat niet klopt.

const nl = {
  meta: {
    title: 'Digitale Duif van Rotterdam',
    description: 'Drie digitale duiven aan tafel. Een experiment van AI-MAPS (Erasmus Universiteit Rotterdam), gemaakt door Parai.',
  },
  skip: 'Naar de duiven',
  noscript: 'Deze pagina heeft JavaScript nodig om de opgenomen gesprekken af te spelen.',

  header: {
    title: 'Digitale Duif van Rotterdam',
    lang: 'Taal',
    login: 'Inloggen',
    loginTitle: 'Inloggen in de werkomgeving',
    theme: 'Donkere weergave',
    brands: 'AI-MAPS en Parai',
  },

  hero: {
    eyebrow: 'Een experiment van AI-MAPS en Parai',
    title: 'Wat gebeurt er met een gesprek als een duif aan tafel zit?',
    line: 'Drie digitale duiven, elk met een eigen manier van reageren. Kies er één: de duif vooraan zit bij jou aan tafel.',
    faq: 'Veelgestelde vragen',
  },

  carousel: {
    label: 'Kies een duif',
    roledesc: 'carrousel',
    prev: 'Vorige duif',
    next: 'Volgende duif',
    autoStop: 'Zet het vanzelf wisselen uit',
    autoStart: 'Laat de duiven vanzelf wisselen',
    current: 'Vooraan: {name}',
    help: 'Wissel met de pijltjestoetsen of klik op een duif opzij.',
  },

  pigeons: {
    kennispartner: {
      name: 'Kennispartner',
      nameThe: 'de Kennispartner',
      label: 'Wetenschappelijke kennis',
      chip: 'Je praat met',
      blurb: 'De duif vertaalt onderzoek naar een duivenperspectief. Elke bewering komt met een bron die je kunt nalezen, en waar de bronnen ophouden, hoor je dat gewoon.',
      placeholder: 'Vraag het de Kennispartner…',
    },
    stakeholder: {
      name: 'Stakeholder',
      nameThe: 'de Stakeholder',
      label: 'Emotie & eigenbelang',
      chip: 'Je praat met',
      blurb: 'De duif komt op voor het eigen belang, uitgesproken en soms ongemakkelijk. De feiten komen uit dezelfde bronnen; het standpunt is door mensen ontworpen. Een facilitator koppelt de woorden van de duif terug naar het gesprek.',
      placeholder: 'Vraag het de Stakeholder…',
    },
    signalerend: {
      name: 'Signalerende duif',
      nameThe: 'de Signalerende duif',
      label: 'Luistert alleen mee',
      chip: 'Aan tafel',
      blurb: 'De duif zit mee in de vergadering, zonder woorden. Soms komt er een reactie, met beweging en echt gekoer. Wat dat betekent, vertaalt niemand voor je.',
      placeholder: '',
    },
  },
  badge: 'Large Pigeon Model',

  modes: {
    label: 'Hoe je de duif ontmoet',
    chat: 'Chat',
    meeting: 'Vergadering',
  },

  chat: {
    log: 'Gesprek met {nameThe}',
    empty: 'Kies een vraag hieronder of typ er zelf een.',
    questions: 'Kies een vraag',
    asked: 'al gesteld',
    input: 'Je vraag',
    send: 'Versturen',
    steps: {
      facilitator: 'Vraag formuleren',
      pigeon_grounding: 'Bronnen lezen',
      pigeon_related_search: 'Verwante bronnen zoeken',
      pigeon: 'Antwoord schrijven',
      mediation: 'Facilitator rondt af',
    },
    stepsLabel: 'Voortgang van dit antwoord',
    stepsBusy: 'Nu bezig: {step}',
    thinking: '{NameThe} denkt na',
    thinkingFacilitator: 'De facilitator denkt na',
    facilitator: 'Facilitator',
    sourcesOne: '1 bron',
    sourcesMany: '{count} bronnen',
    relatedOne: '1 verwante bron',
    relatedMany: '{count} verwante bronnen',
    related: 'Deze bronnen grenzen aan je vraag.',
    gap: 'Geen bron gevonden voor deze vraag.',
    stance: 'Standpunt zonder onderzoek',
    cite: 'Bron {n}',
    compare: 'Vraag het ook aan {nameThe}',
    origin: 'Voortgezet uit je gesprek met {nameThe}',
    back: 'Terug',
    repeated: 'Dezelfde vraag, nu aan {nameThe}',
    own: 'Eigen vragen beantwoorden de duiven in de werkomgeving.',
    ownLogin: 'Inloggen',
    missing: 'Dit antwoord is hier nu niet te zien.',
    loading: 'De opgenomen gesprekken worden geladen…',
    unavailable: 'De opgenomen gesprekken zijn nu niet te laden. Probeer het later nog eens.',
    answered: '{NameThe} heeft geantwoord.',
  },

  honest: {
    recorded: 'Opgenomen in de werkomgeving',
    how: 'Hoe werkt dit?',
  },

  sources: {
    title: 'Bronnen bij dit antwoord',
    from: 'Antwoord van {nameThe}',
    passage: 'De originele woorden uit het paper',
    open: 'Lees het paper',
    graph: 'Bekijk in de kennisgraaf',
    graphNote: 'Na inloggen in de werkomgeving',
    close: 'Sluiten',
  },

  meeting: {
    intro: 'Een korte vergadering over het marktplein. Kies wat er in de zaal gezegd wordt en kijk wat de duif aan tafel doet.',
    atTable: 'Aan tafel: {name}',
    log: 'Verslag van de vergadering',
    empty: 'Nog niets gezegd. Kies hieronder een uitspraak.',
    statements: 'Wat wordt er gezegd?',
    said: 'al gezegd',
    sound: 'Geluid in de vergadering',
    // De schakelaar heet "Geluid"; de stand staat er klein naast.
    soundLabel: 'Geluid',
    soundOn: 'aan',
    soundOff: 'uit',
    soundOffNote: 'Het geluid staat uit.',
    soundTurnOn: 'Zet aan',
    listen: 'Beluister',
    stop: 'Stop',
    signal: 'Signaal van {nameThe}',
    restart: 'Opnieuw beginnen',
    missing: 'Deze reactie is hier nu niet te zien.',
    loading: 'De opgenomen vergadering wordt geladen…',
    unavailable: 'De opgenomen vergadering is nu niet te laden. Probeer het later nog eens.',
  },

  video: {
    eyebrow: 'Werkomgeving',
    title: 'Zo werkt het in de echte omgeving',
    body: 'Een korte rondleiding door de werkomgeving, in het Engels: twee duiven op dezelfde vraag, met bronnen en de kennisgraaf, en een vergadering waarin iemand aan tafel de duif laat reageren.',
    soon: 'Binnenkort',
    soonLabel: 'De video komt binnenkort',
    play: 'Speel de video af',
  },

  research: {
    eyebrow: 'Onderzoek',
    title: 'Over dit project',
    body: 'Digitale Duif van Rotterdam is een experiment van AI-MAPS (Erasmus Universiteit Rotterdam), gemaakt door Parai. Samen met de Universiteit Utrecht verzamelde het project wetenschappelijke kennis over hoe grote evenementen in de stad duiven raken. Het gaat over more-than-human design: hoe geef je een niet-menselijke stem een plek in het gesprek over de stad? Drie duiven, elk met een ander ontwerpstandpunt, maken die vraag tastbaar en bespreekbaar.',
    links: {
      paper: 'Onderzoekspaper (binnenkort)',
      thesis: 'Thesis: duiven & de stad',
      about: 'Over Parai × AI-MAPS',
      download: {
        posters: 'Materiaal voor groepssessies: de posters (pdf, {size} MB)',
        cards: 'Materiaal voor groepssessies: de kaartjes (pdf, {size} MB)',
      },
    },
    postersTitle: 'Posters',
    postersNote: 'Gemaakt voor de AI-HHH-conferentie. Klik op een poster om hem groter te bekijken.',
    postersLabel: 'Posters',
    soon: 'Binnenkort',
    view: 'Bekijk de poster {title} groter',
    posters: [
      {
        title: 'Van papers naar duiven',
        sub: 'De kennisgraaf achter de duiven',
        alt: 'Poster: van 120 wetenschappelijke papers naar 4.067 bevindingen, 26 thema’s en drie vormen van duif. Een uitsnede van de kennisgraaf loopt van het thema milieu en dierenwelzijn via slaap en kunstlicht bij vogels naar één bevinding over melatonine en kunstlicht.',
      },
      {
        title: 'De duif in beleid',
        sub: 'Welke rol hoort de duif te krijgen?',
        alt: 'Poster met de vraag “What role should the Pigeon have in policy making?” tussen drie AI-gegenereerde foto’s van duiven in Rotterdam, waaronder een duif in pak met stropdas en een badge van de gemeente: “Ambtenaar”.',
      },
      {
        title: 'Digital Pigeon for Policy',
        sub: 'Het project en zijn drie duiven',
        alt: 'Poster “Digital Pigeon for Policy”: de duif is overal in de stad, geliefd en gehaat, maar wat weten we er echt van? AI-MAPS begon deze more-than-human-zoektocht met Parai; samen met de Universiteit Utrecht verzamelde het project de wetenschappelijke kennis over wat grote evenementen in de stad met duiven doen. Een schema toont drie vormen van duivenvertegenwoordiging: de Kennispartner, de Stakeholder en de Signalerende duif. Met een QR-code naar deze site en een foto van een onderzoeker tussen de duiven op een plein.',
      },
    ],
    cardsTitle: 'Kaartjes',
    cardsNote: 'Zes vragen om samen over door te praten, gemaakt voor dezelfde conferentie. Klik op een kaartje voor de achterkant.',
    cardView: 'Kaartje {n}: {question} Bekijk voor- en achterkant',
    cardFront: 'Voorkant',
    cardBack: 'Achterkant',
    cards: [
      { front: 'Welke rol hoort de duif te krijgen in beleid?', alt: 'Welke rol hoort de duif te krijgen in beleid? Met drie foto’s van duiven, die volgens het kaartje zelf AI-gegenereerd zijn en geen wilde duiven.', back: 'Is het genoeg om stil te staan bij hoe een duif zich voelt? Of moet de duif ontbrekende kennis aanvullen? Of mee kunnen praten in de discussie?' },
      { front: 'Welke duiven mogen meedoen?', back: 'En zijn alle duiven het dan met elkaar eens?' },
      { front: 'Is deze duif het eens met onze aanpak?', back: 'Moeten we duiven om toestemming vragen, of ze laten weten hoe het proces verloopt?' },
      { front: 'Van LLM naar LPM: hebben we juist een Large Pigeon Model nodig?', back: 'Blijft de stem van de duif altijd onze menselijke weerspiegeling en interpretatie van de duif?' },
      { front: 'Wat leert de deelname van duiven ons over de deelname van mensen?', back: 'Zijn de duiven de hangjongeren van de straat, die pas hun mening geven als er snacks zijn?' },
      { front: 'Mag een duif van mening veranderen?', back: 'Wat is de ware opi(g)nion van de duif?' },
    ],
  },

  viewer: {
    close: 'Sluiten',
    prev: 'Vorige',
    next: 'Volgende',
    count: '{n} van {total}',
    full: 'Op ware grootte openen',
  },

  method: {
    summary: 'Hoe de kennisbank van de duiven is opgebouwd',
    intro: 'De pratende duiven lezen uit één verzameling wetenschappelijke literatuur. Hier staat hoe die verzameling is samengesteld, wat er per publicatie uit is gehaald en waar zij ophoudt.',
    items: [
      {
        id: 'selectie',
        title: 'Wat er in de verzameling terechtkwam',
        body: (f) => `De verzameling bestaat uit wetenschappelijke literatuur over duiven, dieren en de stad. Welke publicaties erin mochten, besliste het projectteam in een eigen leesronde.${f.publications ? ` De uitkomst: ${f.publications} publicaties, waaruit de werkomgeving leest.` : ''}`,
      },
      {
        id: 'claim',
        title: 'Wat als claim telt',
        body: (f) => `Elke publicatie is gelezen op de afzonderlijke uitspraken die zij doet, en elke uitspraak is bewaard naast de passage waar zij vandaan komt. Niet alleen uitspraken over duiven: een bevinding over vogels, over dieren in de stad of over hoe mensen over dieren praten telt ook mee.${f.claims && f.passages ? ` Samen leverden ze ${f.claims} uitspraken en ${f.passages} geciteerde passages op.` : ''} Een uitspraak houdt altijd haar eigen zin, zodat een lezing terug te voeren is op wat het paper werkelijk schreef.`,
      },
      {
        id: 'herkomst',
        title: 'Waar de methode vandaan komt',
        body: () => 'Dit is de interviewmethode van Parai, aangepast. Bij een interview wordt een transcript gelezen op wat iemand werkelijk zegt, los van de manier waarop het gezegd is. Hier neemt een publicatie de plaats van het transcript in en een claim de plaats van de uitspraak. Zo blijft elke publicatie een eigen stem die je kunt terughoren.',
      },
      {
        id: 'soort',
        title: 'Als een paper niet zegt om welke vogel het gaat',
        body: () => 'Onderzoek noemt niet altijd de soort die het bestudeerde. Een bevinding over “vogels” geldt misschien wel en misschien niet voor een stadsduif, en de publicatie zelf beslist dat vaak niet. Die gevallen zijn bewaard in plaats van weggelaten, met de oorspronkelijke formulering erbij. Gebruikt een duif zo’n bevinding, dan toont de bronkaart de passage, zodat je ziet wat er gemeten is en wat er wordt toegepast.',
      },
      {
        id: 'grens',
        title: 'Wat deze literatuur niet beantwoordt',
        body: () => 'De verzameling leunt naar de natuurwetenschappen; de vragen van het project gaan over de stad en de mensen erin. Het lezen bracht vooral gaten en kansen naar boven. Daarom zijn er drie verschillende duiven en niet één autoriteit.',
      },
      {
        id: 'kennisgat',
        title: 'Wat een kennisgat hier betekent',
        body: () => 'Zegt een duif dat iets niet uit de bronnen vast te stellen is, dan gaat die uitspraak over deze verzameling en deze zoekactie. Het is geen bewijs dat er nergens onderzoek bestaat.',
      },
    ],
    note: (f) => (f.subthemes && f.themes && f.domains
      ? `De verzameling is met een hiërarchische k-means-clustering geordend in ${f.subthemes} subthema’s, ${f.themes} thema’s en ${f.domains} domeinen${f.perspectives ? `, naast ${f.perspectives} perspectieven` : ''}. Dat is een ordening van het materiaal, niet het brein of de overtuiging van een duif.`
      : ''),
    graphAlt: 'Een deel van de kennisgraaf: bronnen en uitspraken als verbonden punten.',
    graphCaption: 'Een deel van de kennisgraaf in de werkomgeving.',
  },

  faq: {
    title: 'Veelgestelde vragen',
    close: 'Sluiten',
    // Alleen als config.contact een adres heeft; het adres volgt als link.
    contact: {
      toegang: 'Toegang vraag je aan via',
      representatief: 'Laat het ons weten via',
    },
    items: [
      {
        id: 'waarom',
        q: 'Waarom de duif?',
        a: () => 'De duif is overal in de stad, en juist daardoor bijna onzichtbaar. Voor de een is de duif een plaag, voor de ander, vaak een jongere die tussen het beton opgroeit, een vertrouwde stadsgenoot. Er bestaat geen enkel ‘juist’ beeld van de duif: ongedierte, buur, symbool en overlever tegelijk. Precies die meerstemmigheid maakt de duif een goede ingang om een more-than-human perspectief bespreekbaar te maken.',
      },
      {
        id: 'herkomst',
        q: 'Waar komen deze duiven vandaan?',
        a: () => 'Uit een experiment van AI-MAPS, het consortium over AI en publieke veiligheid dat de Erasmus Universiteit Rotterdam leidt. Parai heeft de duiven gemaakt. De vraag erachter komt uit het more-than-human design: hoe geef je een niet-menselijke stem een plek in het gesprek over de stad? Elke duif is een prototype van een andere ontwerpkeuze. De Kennispartner blijft dicht bij het onderzoek, de Stakeholder neemt bewust een standpunt in en de Signalerende duif doet mee zonder woorden.',
      },
      {
        id: 'deelname',
        q: 'Hebben er echte duiven meegedaan?',
        a: ({ facts: f }) => `Geen levende duif heeft bijgedragen aan wat de duiven hier zeggen. De Kennispartner leest uit een verzameling van ${f.publications ? `${f.publications} ` : ''}wetenschappelijke publicaties over duiven, dieren en de stad. De Stakeholder haalt de feiten uit dezelfde bronnen, maar standpunt en toon zijn door mensen ontworpen. De Signalerende duif zegt niets. De duif beweegt en koert met echte veldopnames van duiven, en niemand vertaalt wat dat betekent.`,
      },
      {
        id: 'representatief',
        q: 'Zijn deze duiven representatief?',
        a: () => 'Nee, en dat is precies het punt. Eigen ervaring, verschillen tussen duiven, emotie en context laten zich niet uit papers halen. De duiven laten zien wat we weten en waar die kennis ophoudt. Zegt een duif dat de bronnen iets niet weten, dan gaat dat over deze verzameling en niet over al het onderzoek dat bestaat. Zie je een gat? Dan is dat een uitnodiging om samen aan een rijker beeld te bouwen.',
      },
      {
        id: 'echt',
        q: 'Zijn de antwoorden op deze pagina echt?',
        a: ({ recording: r }) => `Ja. Elke zin van een duif of van de facilitator is opgenomen in de werkomgeving, op precies de vragen en uitspraken die je hier ziet.${r.month ? ` Opgenomen in ${r.month}.` : ''} Wij kozen alleen welke opname hier staat en kortten de wachttijd in; de tekst en de bronnen zijn niet veranderd. De reacties in de vergadering zijn per uitspraak in een eigen, verse vergadering opgenomen, zodat een reactie niet afhangt van de volgorde waarin je klikt. De stem is dezelfde synthetische stem als in de werkomgeving. Deze pagina stuurt zelf niets naar een taalmodel. Wat je zelf typt, blijft in je browser en is weg als je de pagina sluit.`,
      },
      {
        id: 'straat',
        q: 'Zijn jullie echt duiven op straat gaan vragen?',
        a: () => 'Ja, en dat was een onderzoeksoefening, geen dataverzameling. Mensen uit het project benaderden duiven in Rotterdam met toestemmingsformulieren en vragen, precies zoals zij een menselijke deelnemer zouden benaderen. Niets van wat een duif die dag deed, is in de kennisbank terechtgekomen. Het ging erom in het openbaar te voelen wat toestemming en representatie betekenen wanneer de deelnemer nergens mee kan instemmen.',
      },
      {
        id: 'toegang',
        q: 'Hoe kom ik in de werkomgeving?',
        a: () => 'Via Inloggen, rechtsboven op deze pagina. Toegang krijg je via het projectteam. In de werkomgeving stel je je eigen vragen, open je de kennisgraaf en neem je een duif mee naar een echte vergadering.',
      },
    ],
  },

  footer: {
    line: 'Een experiment van AI-MAPS · Erasmus Universiteit Rotterdam · gemaakt door Parai',
    images: 'Beeld',
  },

  // Een knipoog, geen onderdeel van het verhaal: vijf keer op het AI-MAPS-logo
  // (digipigie.js).
  digipigie: {
    title: 'DigiPigie',
    thanks: 'Voor al je hulp: dank je wel, van ons en van de duiven.',
    close: 'Terug naar de pagina',
  },

  print: {
    title: 'Digitale Duif van Rotterdam',
    working: 'Werkversie · september 2026',
    note: 'Voor groepssessies. Een experiment van AI-MAPS (Erasmus Universiteit Rotterdam), gemaakt door Parai.',
    print: 'Print deze pagina',
    back: 'Terug naar de etalage',
    kinds: { poster: 'Poster', cards: 'Reflectiekaarten' },
    kindLabel: 'Soort materiaal',
    intro: 'Kies een gedeeld besluit. Ontmoet meer dan één duif. Pauzeer en bespreek wat er veranderde.',
    modes: 'Chat · Vergadering',
    modesSignal: 'Vergadering',
    cards: [
      {
        id: 'signaal',
        title: 'Is een signaal genoeg?',
        action: 'Laat de Signalerende duif één keer reageren in de vergadering en pauzeer.',
        question: 'Wat merkte je op? Welke betekenis gaf je er zelf aan?',
      },
      {
        id: 'kennis',
        title: 'Wanneer is kennis nodig?',
        action: 'Open een bron bij een antwoord van de Kennispartner.',
        question: 'Wat ondersteunt deze studie? Wat heb je nog nodig voor je eigen situatie?',
      },
      {
        id: 'spreker',
        title: 'Wie spreekt?',
        action: 'Stel dezelfde vraag aan de Kennispartner en aan de Stakeholder.',
        question: 'Waar eindigt onderzoek en begint een ontworpen standpunt?',
      },
      {
        id: 'ontbreekt',
        title: 'Wie ontbreekt?',
        action: 'Benoem het besluit dat jullie groep bespreekt.',
        question: 'Wiens belangen zijn makkelijk hoorbaar? Wie ontbreekt in deze ontmoeting?',
      },
      {
        id: 'stem',
        title: 'Wat doet een stem?',
        action: 'Luister naar een gesproken reactie in de vergadering en lees daarna alleen de tekst.',
        question: 'Verandert je oordeel over gezag of persoonlijkheid als je de stem hoort?',
      },
      {
        id: 'toegevoegd',
        title: 'Wat voegden wij toe?',
        action: 'Schrijf één waarneming en één interpretatie op papier.',
        question: 'Wat is het verschil tussen die twee?',
      },
    ],
    demoNote: 'De antwoorden op de website zijn opgenomen in de werkomgeving. De duiven nodigen uit tot reflectie; ze laten niet zien wat een levende duif denkt.',
  },
};

const en = {
  meta: {
    title: 'Digital Pigeon of Rotterdam',
    description: 'Three digital pigeons at the table. An experiment by AI-MAPS (Erasmus University Rotterdam), made by Parai.',
  },
  skip: 'Skip to the pigeons',
  noscript: 'This page needs JavaScript to play the recorded conversations.',

  header: {
    title: 'Digital Pigeon of Rotterdam',
    lang: 'Language',
    login: 'Log in',
    loginTitle: 'Log in to the workspace',
    theme: 'Dark mode',
    brands: 'AI-MAPS and Parai',
  },

  hero: {
    eyebrow: 'An experiment by AI-MAPS and Parai',
    title: 'What happens to a conversation when a pigeon gets a seat at the table?',
    line: 'Three digital pigeons, each with its own way of responding. Pick one: the pigeon up front is the one at your table.',
    faq: 'Frequently asked questions',
  },

  carousel: {
    label: 'Choose a pigeon',
    roledesc: 'carousel',
    prev: 'Previous pigeon',
    next: 'Next pigeon',
    autoStop: 'Turn off auto-rotation',
    autoStart: 'Rotate the pigeons automatically',
    current: 'Up front: {name}',
    help: 'Switch with the arrow keys or click a pigeon at the side.',
  },

  pigeons: {
    kennispartner: {
      name: 'Knowledge Partner',
      nameThe: 'the Knowledge Partner',
      label: 'Scientific knowledge',
      chip: 'Talking with',
      blurb: 'It translates research into a pigeon’s perspective. Every claim comes with a source you can check, and where the sources stop, it simply says so.',
      placeholder: 'Ask the Knowledge Partner…',
    },
    stakeholder: {
      name: 'Stakeholder',
      nameThe: 'the Stakeholder',
      label: 'Emotion & self-interest',
      chip: 'Talking with',
      blurb: 'It stands up for its own interests, outspoken and sometimes uncomfortable. Its facts come from the same sources; its position was designed by people. A facilitator connects its words back to the conversation.',
      placeholder: 'Ask the Stakeholder…',
    },
    signalerend: {
      name: 'Signaling Pigeon',
      nameThe: 'the Signaling Pigeon',
      label: 'Only listens in',
      chip: 'At the table',
      blurb: 'It sits in on the meeting, without words. Sometimes it responds, with movement and real cooing. Nobody translates what that means for you.',
      placeholder: '',
    },
  },
  badge: 'Large Pigeon Model',

  modes: {
    label: 'How you meet the pigeon',
    chat: 'Chat',
    meeting: 'Meeting',
  },

  chat: {
    log: 'Conversation with {nameThe}',
    empty: 'Pick a question below or type your own.',
    questions: 'Pick a question',
    asked: 'already asked',
    input: 'Your question',
    send: 'Send',
    steps: {
      facilitator: 'Working out the question',
      pigeon_grounding: 'Reading sources',
      pigeon_related_search: 'Looking for related sources',
      pigeon: 'Writing the answer',
      mediation: 'The facilitator is wrapping up',
    },
    stepsLabel: 'Progress of this answer',
    stepsBusy: 'Now: {step}',
    thinking: '{NameThe} is thinking',
    thinkingFacilitator: 'The facilitator is thinking',
    facilitator: 'Facilitator',
    sourcesOne: '1 source',
    sourcesMany: '{count} sources',
    relatedOne: '1 related source',
    relatedMany: '{count} related sources',
    related: 'These sources border on your question.',
    gap: 'No source found for this question.',
    stance: 'Position without research',
    cite: 'Source {n}',
    compare: 'Ask {nameThe} too',
    origin: 'Continued from your conversation with {nameThe}',
    back: 'Back',
    repeated: 'The same question, now to {nameThe}',
    own: 'The pigeons answer your own questions in the workspace.',
    ownLogin: 'Log in',
    missing: 'This answer can’t be shown here right now.',
    loading: 'Loading the recorded conversations…',
    unavailable: 'The recorded conversations can’t be loaded right now. Please try again later.',
    answered: '{NameThe} has answered.',
  },

  honest: {
    recorded: 'Recorded in the workspace',
    how: 'How does this work?',
  },

  sources: {
    title: 'Sources for this answer',
    from: 'Answer by {nameThe}',
    passage: 'The original words in the paper',
    open: 'Read the paper',
    graph: 'View in the knowledge graph',
    graphNote: 'After logging in to the workspace',
    close: 'Close',
  },

  meeting: {
    intro: 'A short meeting about the market square. Choose what gets said in the room and watch what the pigeon at the table does.',
    atTable: 'At the table: {name}',
    log: 'Meeting transcript',
    empty: 'Nothing said yet. Pick a statement below.',
    statements: 'What gets said?',
    said: 'already said',
    sound: 'Sound in the meeting',
    soundLabel: 'Sound',
    soundOn: 'on',
    soundOff: 'off',
    soundOffNote: 'Sound is off.',
    soundTurnOn: 'Turn on',
    listen: 'Listen',
    stop: 'Stop',
    signal: 'Signal from {nameThe}',
    restart: 'Start again',
    missing: 'This response can’t be shown here right now.',
    loading: 'Loading the recorded meeting…',
    unavailable: 'The recorded meeting can’t be loaded right now. Please try again later.',
  },

  video: {
    eyebrow: 'Workspace',
    title: 'How it works in the real environment',
    body: 'A short tour of the workspace: two pigeons on the same question, with sources and the knowledge graph, and a meeting in which someone at the table lets the pigeon respond.',
    soon: 'Coming soon',
    soonLabel: 'The video is coming soon',
    play: 'Play the video',
  },

  research: {
    eyebrow: 'Research',
    title: 'About this project',
    body: 'Digital Pigeon of Rotterdam is an experiment by AI-MAPS (Erasmus University Rotterdam), made by Parai. Together with Utrecht University, the project gathered academic knowledge on how big events in the city affect pigeons. It is about more-than-human design: how do you give a non-human voice a place in the conversation about the city? Three pigeons, each with a different design stance, make that question tangible and open to discussion.',
    links: {
      paper: 'Research paper (coming soon)',
      thesis: 'Thesis: pigeons & the city',
      about: 'About Parai × AI-MAPS',
      download: {
        posters: 'Material for group sessions: the posters (pdf, {size} MB)',
        cards: 'Material for group sessions: the postcards (pdf, {size} MB)',
      },
    },
    postersTitle: 'Posters',
    postersNote: 'Made for the AI-HHH conference. Click a poster to see it larger.',
    postersLabel: 'Posters',
    soon: 'Coming soon',
    view: 'View the poster {title} larger',
    posters: [
      {
        title: 'From papers to pigeons',
        sub: 'The knowledge graph behind the pigeons',
        alt: 'Poster: from 120 academic papers to 4,067 findings, 26 themes and three forms of pigeon. A section of the knowledge graph runs from the theme of environment and animal welfare through sleep and artificial light in birds to a single finding about melatonin and artificial light.',
      },
      {
        title: 'The pigeon in policy',
        sub: 'What role should the pigeon have?',
        alt: 'Poster asking “What role should the Pigeon have in policy making?” between three AI-generated photos of pigeons in Rotterdam, one of them a pigeon in a suit and tie with a municipal badge reading “Ambtenaar” (civil servant).',
      },
      {
        title: 'Digital Pigeon for Policy',
        sub: 'The project and its three pigeons',
        alt: 'Poster “Digital Pigeon for Policy”: the pigeon is everywhere in the city, loved and hated, but what do we really know about it? AI-MAPS started this more-than-human quest with Parai; together with Utrecht University, the project gathered the academic knowledge on how big events in the city affect pigeons. A diagram shows three forms of pigeon representation: the Knowledge Partner, the Stakeholder and the Signaling Pigeon. With a QR code to this site and a photo of a researcher among the pigeons on a square.',
      },
    ],
    cardsTitle: 'Postcards',
    cardsNote: 'Six questions to talk through together, made for the same conference. Click a card to see its back.',
    cardView: 'Card {n}: {question} View front and back',
    cardFront: 'Front',
    cardBack: 'Back',
    cards: [
      { front: 'What role should the Pigeon have in policy making?', alt: 'What role should the Pigeon have in policy making? With three photos of pigeons which, the card notes, are AI generated, not wild pigeons.', back: 'Is it enough to reflect on how a pigeon feels? Or should it provide missing knowledge? Or be able to debate within discussion?' },
      { front: 'Which pigeons can participate?', back: 'And do all pigeons then agree with each other?' },
      { front: 'Does this pigeon agree with our approach?', back: 'Should we ask pigeons consent or give them feedback on the process?' },
      { front: 'LLM to LPM: do we need a Large Pigeon Model instead?', back: 'Will the voice of the pigeon always be our human reflection and interpretation of the pigeon?' },
      { front: 'What can we learn from pigeon participation for human participation?', back: 'Are the pigeons the ‘hangjongeren’ of the street that express their opinions only once they get snacks?' },
      { front: 'May a pigeon change its mind?', back: 'What is its true opi(g)nion?' },
    ],
  },

  viewer: {
    close: 'Close',
    prev: 'Previous',
    next: 'Next',
    count: '{n} of {total}',
    full: 'Open at full size',
  },

  method: {
    summary: 'How the pigeons’ knowledge base was built',
    intro: 'The speaking pigeons read from one collection of academic literature. This is how that collection was assembled, what was taken out of each publication and where it stops.',
    items: [
      {
        id: 'selectie',
        title: 'What went into the collection',
        body: (f) => `The collection is academic literature about pigeons, animals and the city. The project team decided in its own reading round which publications qualified.${f.publications ? ` The result: ${f.publications} publications, which the workspace reads from.` : ''}`,
      },
      {
        id: 'claim',
        title: 'What counts as a claim',
        body: (f) => `Every publication was read for the separate statements it makes, and each statement was kept beside the passage it came from. Not only statements about pigeons: a finding about birds, about animals in cities or about how people talk about animals is kept as well.${f.claims && f.passages ? ` Together they yielded ${f.claims} statements and ${f.passages} quoted passages.` : ''} A statement always keeps its own sentence, so a reading can be traced back to what the paper actually wrote.`,
      },
      {
        id: 'herkomst',
        title: 'Where the method comes from',
        body: () => 'This is Parai’s interview method, adapted. In an interview, a transcript is read for what a person actually says, separate from the way they said it. Here a publication takes the place of the transcript and a claim takes the place of the statement. That way each publication stays a voice of its own that you can go back to.',
      },
      {
        id: 'soort',
        title: 'When a paper does not say which bird',
        body: () => 'Research does not always name the species it studied. A finding about “birds” may or may not hold for a city pigeon, and the publication itself often does not settle that. Those cases were kept rather than dropped, with the original wording attached. When a pigeon uses such a finding, the source card shows the passage, so you can see what was measured and what is being applied.',
      },
      {
        id: 'grens',
        title: 'What this literature does not answer',
        body: () => 'The collection leans towards the natural sciences; the project’s questions are about the city and the people in it. Reading it mostly surfaced gaps and opportunities. That is why there are three different pigeons and not one authority.',
      },
      {
        id: 'kennisgat',
        title: 'What a knowledge gap means here',
        body: () => 'When a pigeon says it cannot establish something from its sources, that is a statement about this collection and this search. It is not evidence that no research exists anywhere.',
      },
    ],
    note: (f) => (f.subthemes && f.themes && f.domains
      ? `The collection is grouped by a hierarchical k-means clustering into ${f.subthemes} subthemes, ${f.themes} themes and ${f.domains} domains${f.perspectives ? `, alongside ${f.perspectives} perspectives` : ''}. That is an organisation of the material, not a pigeon’s mind or conviction.`
      : ''),
    graphAlt: 'Part of the knowledge graph: sources and claims as connected points.',
    graphCaption: 'Part of the knowledge graph in the workspace.',
  },

  faq: {
    title: 'Frequently asked questions',
    close: 'Close',
    contact: {
      toegang: 'Ask for access via',
      representatief: 'Let us know via',
    },
    items: [
      {
        id: 'waarom',
        q: 'Why the pigeon?',
        a: () => 'The pigeon is everywhere in the city, and for that very reason almost invisible. To one person it is a pest; to another, often a young person growing up amid the concrete, a familiar fellow citizen. There is no single ‘right’ image of the pigeon: it is vermin, neighbour, symbol and survivor all at once. That very multiplicity makes it a good way in to discussing a more-than-human perspective.',
      },
      {
        id: 'herkomst',
        q: 'Where do these pigeons come from?',
        a: () => 'From an experiment by AI-MAPS, the consortium on AI and public safety led by Erasmus University Rotterdam. Parai made the pigeons. The question behind them comes from more-than-human design: how do you give a non-human voice a place in the conversation about the city? Each pigeon is a prototype of a different design choice. The Knowledge Partner stays close to the research, the Stakeholder deliberately takes a position and the Signaling Pigeon joins in without words.',
      },
      {
        id: 'deelname',
        q: 'Did real pigeons take part?',
        a: ({ facts: f }) => `No living pigeon contributed to what these pigeons say. The Knowledge Partner reads from a collection of ${f.publications ? `${f.publications} ` : ''}academic publications about pigeons, animals and the city. The Stakeholder takes its facts from the same sources, but its position and tone were designed by people. The Signaling Pigeon says nothing. It moves and coos with real field recordings of pigeons, and nobody translates what that means.`,
      },
      {
        id: 'representatief',
        q: 'Are these pigeons representative?',
        a: () => 'No, and that is exactly the point. Lived experience, differences between pigeons, emotion and context can’t be distilled from papers. The pigeons show what we know and where that knowledge stops. When a pigeon says its sources don’t know something, that is about this collection and not about all the research that exists. Spot a gap? Treat it as an invitation to build a richer picture together.',
      },
      {
        id: 'echt',
        q: 'Are the answers on this page real?',
        a: ({ recording: r }) => `Yes. Every sentence from a pigeon or the facilitator was recorded in the workspace, in response to exactly the questions and statements you see here.${r.month ? ` Recorded in ${r.month}.` : ''} We only chose which recording appears here and shortened the waiting time; the text and the sources are unchanged. The meeting responses were recorded one statement at a time, each in a fresh meeting of its own, so a response does not depend on the order in which you click. The voice is the same synthetic voice as in the workspace. This page sends nothing to a language model itself. Whatever you type stays in your browser and is gone when you close the page.`,
      },
      {
        id: 'straat',
        q: 'Did you really go and ask pigeons on the street?',
        a: () => 'Yes, and it was a research exercise, not data collection. Members of the project approached pigeons in Rotterdam with consent forms and questions, exactly as they would approach a human participant. Nothing a pigeon did that day entered the knowledge base. The point was to feel, in public and in practice, what consent and representation mean when the participant cannot consent to anything.',
      },
      {
        id: 'toegang',
        q: 'How do I get into the workspace?',
        a: () => 'Through Log in, at the top right of this page. Access is arranged by the project team. In the workspace you ask your own questions, open the knowledge graph and bring a pigeon to a real meeting.',
      },
    ],
  },

  footer: {
    line: 'An experiment by AI-MAPS · Erasmus University Rotterdam · made by Parai',
    images: 'Images',
  },

  digipigie: {
    title: 'DigiPigie',
    thanks: 'For all your help: thank you, from us and from the pigeons.',
    close: 'Back to the page',
  },

  print: {
    title: 'Digital Pigeon of Rotterdam',
    working: 'Working edition · September 2026',
    note: 'For group sessions. An experiment by AI-MAPS (Erasmus University Rotterdam), made by Parai.',
    print: 'Print this page',
    back: 'Back to the showcase',
    kinds: { poster: 'Poster', cards: 'Reflection cards' },
    kindLabel: 'Kind of material',
    intro: 'Choose a shared decision. Meet more than one pigeon. Pause and discuss what changed.',
    modes: 'Chat · Meeting',
    modesSignal: 'Meeting',
    cards: [
      {
        id: 'signaal',
        title: 'Is a signal enough?',
        action: 'Let the Signaling Pigeon respond once in the meeting, then pause.',
        question: 'What did you notice? Which meaning did you supply yourself?',
      },
      {
        id: 'kennis',
        title: 'When do we need knowledge?',
        action: 'Open a source beside an answer from the Knowledge Partner.',
        question: 'What does this study support? What would you still need for your own situation?',
      },
      {
        id: 'spreker',
        title: 'Who is speaking?',
        action: 'Ask the Knowledge Partner and the Stakeholder the same question.',
        question: 'Where does research end and a designed position begin?',
      },
      {
        id: 'ontbreekt',
        title: 'Who is missing?',
        action: 'Name the decision your group is discussing.',
        question: 'Whose interests are easy to hear? Who is absent from this encounter?',
      },
      {
        id: 'stem',
        title: 'What does a voice do?',
        action: 'Listen to a spoken response in the meeting, then read only the text.',
        question: 'Does hearing the voice change how you judge authority or personality?',
      },
      {
        id: 'toegevoegd',
        title: 'What did we add?',
        action: 'Write one observation and one interpretation on paper.',
        question: 'What is the difference between the two?',
      },
    ],
    demoNote: 'The answers on the website were recorded in the workspace. The pigeons invite reflection; they do not show what a living pigeon thinks.',
  },
};

export const COPY = { nl, en };
export const LANGS = ['nl', 'en'];

// Vult {naam}-plekken. {NameThe} is dezelfde waarde als {nameThe} met een
// hoofdletter, voor het begin van een zin ("De Kennispartner heeft geantwoord").
export function fill(template, vars = {}) {
  return template.replace(/\{(\w+)\}/g, (whole, key) => {
    if (key in vars) return String(vars[key]);
    const lower = key.charAt(0).toLowerCase() + key.slice(1);
    if (lower !== key && lower in vars) {
      const value = String(vars[lower]);
      return value.charAt(0).toUpperCase() + value.slice(1);
    }
    return whole;
  });
}

const LOCALES = { nl: 'nl-NL', en: 'en-GB' };

export function formatNumber(lang, value) {
  return new Intl.NumberFormat(LOCALES[lang]).format(value);
}

// "september 2026" / "September 2026", in Nederlandse tijd.
export function formatMonth(lang, iso) {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  return new Intl.DateTimeFormat(LOCALES[lang], { month: 'long', year: 'numeric', timeZone: 'Europe/Amsterdam' }).format(date);
}

// De feiten uit opnames.json, opgemaakt voor de taal. Wat ontbreekt, blijft weg.
// `publications` is wat de publieke tekst noemt: het aantal verschillende werken
// (facts.works); twee bronknopen in de kennisbank zijn hetzelfde werk. Ontbreekt
// works, dan het aantal bronnen.
export function formatFacts(lang, facts) {
  const out = {};
  for (const [key, value] of Object.entries(facts || {})) {
    if (Number.isFinite(value) && value >= 0) out[key] = formatNumber(lang, value);
  }
  const publications = out.works || out.sources;
  if (publications) out.publications = publications;
  return out;
}
