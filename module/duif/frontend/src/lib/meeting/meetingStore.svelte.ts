import mediaCatalogue from 'virtual:duif-media'
import { AudioSluice } from '../audio/mic'
import { PcmTurn, SoundBoard, TtsCollector, TtsPlayer, type PlaybackState } from '../audio/player'
import { API, type ApiFailure, jsonInit, readFailure } from '../api'
import { type Bilingual, isLang, isMsgKey, type Lang, type MsgKey, type UiLocale } from '../i18n.svelte'
import { type BehaviourDTO, fetchPigeons, type Pigeon } from '../pigeons'
import { type KBStatus, parseKBStatus, parseSourceDrops, parseSources, type Source, type SourceDrops } from '../sources'
import { SIGNAL_BANDS, type SignalBand } from '../media/types'
import type { AudioContextLike, EventSourceLike, MeetingPorts, MicLike } from './ports'
import {
  AUDIO_FORMATS,
  type AnswerStopReason,
  CAPACITY_SELF,
  CLOSE_REASONS,
  type Eagerness,
  EVENTS,
  isEagerness,
  isListenState,
  isVoiceState,
  LISTEN,
  type ListenState,
  MAX_COOLDOWN_SECONDS,
  type MeetingCandidate,
  type MeetingEvent,
  parseCandidate,
  ROUTES,
  type SettingsDTO,
  type TopicRef,
  TTS_UNAVAILABLE,
  TURN_TIMEOUT,
  VOICE,
  type VoiceState,
} from './protocol'

// Het scherm is geen archief: wat eraf valt is weg, er bestaat geen kopie.
const MAX_TRANSCRIPT = 80
const MAX_ANSWERS = 12
const DEVICE_CHECK_MS = 3000

// Een schuif die beweegt is één bedoeling, geen dertig. De POST volgt zodra de
// hand stilligt; de knoppen zelf reageren meteen.
export const SETTINGS_DEBOUNCE_MS = 250

// MIC_FLUSH_MS is de tijd die de spreekknop de microfoonsluis geeft om haar
// laatste blokken weg te sturen vóór hij om een beurt vraagt. De
// sluis mag tot drie seconden audio onderweg hebben (MAX_SLUICE_BLOCKS), en
// juist de zin vlak vóór de knopdruk is meestal de vraag aan de duif — die zin
// moet de server dus wel bereikt hebben voordat hij zijn venster leest. Het is
// bewust een plafond en geen belofte: normaal staat er hooguit één blok klaar
// en is de sluis binnen een fractie leeg, en een haperende lijn mag de knop
// niet ophouden. De knop zelf voelt meteen ingedrukt — de stand "de duif denkt"
// staat er al vóór dit wachten.
export const MIC_FLUSH_MS = 800

// De inactiviteitsbewaking. De server is de enige klok: hij stelt
// "Ben je er nog?" en hij sluit de vergadering. De browser meldt alleen dat er
// iemand aan het scherm zit — een klik, een toets, een aanraking — en dat
// hoogstens eens per INTERACTION_THROTTLE_MS; op een vraag van tien minuten
// scheelt dat niets, en het scheelt een verzoek per toetsaanslag.
export const INTERACTION_THROTTLE_MS = 30_000
// De aftelling tekent zich eens per seconde opnieuw, maar rekent met de klok:
// een geremde timer in een achtergrondtabblad laat hooguit een tel liggen.
const PRESENCE_REDRAW_MS = 1000
// De lokale terugval: is de aftelling zoveel voorbij en kwam er geen
// session_closed (het netwerk is weg), dan legt de browser de vergadering zelf
// neer, langs hetzelfde pad als elke andere sluiting.
export const PRESENCE_FALLBACK_MS = 10_000
// Onder deze resterende tijd zegt de dialoog het één keer hardop tegen een
// schermlezer; de seconden zelf leest hij niet voor.
export const PRESENCE_SOON_MS = 30_000

// connection_lost is een clientwaarheid, geen serverreden: de server heeft de
// sessie niet gesloten, wij kunnen hem alleen niet meer horen.
export const CLOSE_CONNECTION_LOST = 'connection_lost'

// De microfoon zoals de gebruiker hem ervaart. De luisterindicator is hiervan
// een afgeleide: een hartslag van de server kan een geweigerde of
// gedempte microfoon nooit groen maken.
export type MicState = 'closed' | 'opening' | 'open' | 'muted' | 'refused'

export interface TranscriptLine {
  seq: number
  speaker: string
  text: string
  own: boolean
}

export interface AnswerLine {
  id: number
  text: string
  note: string
  pigeonId: string
  // De beurt waar dit antwoord bij hoort: de server noemt hem op elk bericht
  // van die beurt, zodat een laat bericht niet op de verkeerde kaart landt.
  // Leeg wanneer de server geen turnId gaf.
  turnId?: string
  // De transcriptregel waar dit antwoord op volgde. Het sessieverloop zet
  // gesproken regels en bijdragen op één lijn; zonder dit veld zou een bijdrage
  // altijd onderaan belanden, ook als er daarna nog gepraat is.
  afterSeq?: number
  sources: Source[]
  kbStatus?: KBStatus
  // dropped meldt dat er gevonden bronnen wegvielen vóór deze lijst.
  // Hij hoort bij deze bijdrage en gaat, net als de rest van de vergadering,
  // nergens heen: de zaal ziet hem zolang de bijdrage op het scherm staat.
  dropped?: SourceDrops
  status: 'streaming' | 'complete' | 'stopped' | 'interrupted'
  // startedAt is het moment waarop deze bijdrage geopend werd. De kaart leest
  // het om na een tijdje te tonen hoe lang het voorbereiden al duurt: de
  // modelcall achter een bijdrage kan seconden of een minuut kosten, en een
  // stand die dat verzwijgt leest als een vastgelopen scherm.
  startedAt: number
  // Providerafsluiting blijft bij dit antwoord, los van de lokale Stop.
  stopReason?: AnswerStopReason
}

export interface MeetingSettingsState {
  autonomous: boolean
  eagerness: Eagerness
  cooldownSeconds: number
}

// MeetingNotice is de ene melding die het statusgebied laat zien. `action`
// zegt welke herstelknop erbij hoort; zonder actie is er niets te drukken.
export interface MeetingNotice {
  id: string
  tone: 'danger' | 'warning' | 'secondary'
  text: string
  action?: 'sound' | 'microphone'
}

// De vier standen die de gebruiker van een lopende vergadering hoeft te kennen,
// plus 'attention' voor alles wat aandacht vraagt. Elf nuances op één regel
// leest niemand; dit leest iedereen.
export type MeetingStatus = 'attention' | 'speaking' | 'thinking' | 'listening' | 'paused'

interface OpenResponse {
  sessionId?: string
  pigeonId?: string
  lang?: string
  listening?: unknown
  voice?: unknown
  autonomous?: boolean
  eagerness?: string
  settings?: SettingsDTO
  autonomyPaused?: boolean
  playbackTracking?: boolean
}

function clampCooldown(value: unknown): number {
  const n = typeof value === 'number' && Number.isFinite(value) ? Math.round(value) : 0
  return n < 0 ? 0 : n > MAX_COOLDOWN_SECONDS ? MAX_COOLDOWN_SECONDS : n
}

// MeetingStore is de vergadering zoals de client hem kent: één exemplaar per
// mount, alle browser-API's via ports, geen $effect. Twee regels sturen dit
// bestand:
//
//  1. Audio en transcript zijn streaming-only. Microfoonblokken
//     gaan meteen als bytes de lijn op; transcript en antwoorden leven in
//     $state.raw-arrays die bij stop, einde en unmount worden losgelaten.
//  2. De knop is de gegarandeerde basis. Wat er ook wegvalt (STT,
//     microfoon, detector, stem, budget): de bediening blijft staan en de
//     stopknop werkt lokaal, zonder op de server te wachten.
// DEFAULT_PCM_RATE is de sample rate van de Soniox-stem; de server zet hem op
// elk frame, dit is het vangnet.
const DEFAULT_PCM_RATE = 24000

export class MeetingStore {
  // --- sessie ---------------------------------------------------------------
  live = $state(false)
  disposed = $state(true)
  phase = $state<'setup' | 'opening' | 'live'>('setup')
  sessionId = $state('')
  // pigeonId is een accessor en geen kaal veld, omdat de duif kiezen op de
  // opzetkaart meer is dan een naam wisselen: zelfstandig meedoen heeft per
  // duif een eigen standaardstand, en die hoort bij de duif die er
  // op dat moment staat. Zowel het keuzemenu als de schil (App.svelte) schrijft
  // hierin, dus één zetter is ook de enige plek die dat hoeft te weten.
  private pigeonIdState = $state('')
  get pigeonId(): string {
    return this.pigeonIdState
  }
  set pigeonId(next: string) {
    if (next === this.pigeonIdState) return
    this.pigeonIdState = next
    this.applyPigeonDefaults()
  }
  lang = $state<Lang>('en')
  voice = $state<VoiceState>(VOICE.off)
  closedReason = $state('')

  // --- opzet ----------------------------------------------------------------
  pigeons = $state.raw<Pigeon[]>([])
  pigeonsLoaded = $state(false)
  pigeonsFailed = $state(false)
  autonomyPaused = $state(false)
  resumeInFlight = $state(false)
  playbackTracking = $state(false)
  playbackStatus = $state<'idle' | 'pending' | PlaybackState>('idle')
  // playbackSyncFailed gaat over geluid: de server meldde een afspeeltijdslimiet,
  // of hij nam een afspeelbevestiging niet aan. stopSyncFailed gaat over de
  // bediening: de stopknop werkte hier wel, maar bereikte de server niet. Die
  // twee zijn één melding geweest en dat las als "het geluid speelde niet
  // volledig af" over een storing die niets met geluid te maken had.
  playbackSyncFailed = $state(false)
  stopSyncFailed = $state(false)
  activeTurnId = $state('')
  settings = $state<MeetingSettingsState>({ autonomous: false, eagerness: 'normal', cooldownSeconds: 0 })

  // --- luisteren -------------------------------------------------------
  serverListen = $state<ListenState>(LISTEN.off)
  localMic = $state<MicState>('closed')
  muted = $state(false)
  micStream = $state<MediaStream | null>(null)
  // orbVisible: de VoiceOrb draait een eigen analyser + rAF-lus; zodra de
  // vergadertab niet zichtbaar is krijgt hij geen stream (App zet dit).
  orbVisible = $state(true)
  droppedLocal = $state(0)
  droppedServer = $state(0)

  // --- spreekbeurt ----------------------------------------------------------
  speaking = $state(false)
  stopped = $state(false)
  // Monotonic local cancellation also stops a finite visual after audio ended.
  mediaStopRevision = $state(0)
  stopPending = $state(false)
  respondInFlight = $state(false)
  transcript = $state.raw<TranscriptLine[]>([])
  signal = $state<{ id: string; durationMs: number } | null>(null)
  signalError = $state(false)
  // --- vastzitten en wisselen -------------------------------------
  // stuckConfirm staat open zodra de gebruiker de uitweg uit de
  // capaciteitsmelding heeft aangeklikt; pas het "ja" sluit de andere
  // vergadering. replaceInFlight houdt de knop bezet zolang dat loopt.
  stuckConfirm = $state(false)
  replaceInFlight = $state(false)
  // --- beëindigen --------------------------------------------------
  // Sinds de knoppen van plaats wisselden is beëindigen de grote rode knop, en
  // die staat waar eerder "Stop" stond. Spreekt de duif nog, dan is de klik
  // waarschijnlijk bedoeld om háár stil te krijgen en niet om de vergadering te
  // sluiten; endConfirm houdt die ene vraag open. Zwijgt zij, dan beëindigt de
  // knop meteen — een vraag stellen waar niets te verliezen valt, leert de zaal
  // alleen maar wegklikken.
  endConfirm = $state(false)
  // pendingSwitch is de duif die de gebruiker aanwees terwijl de vergadering
  // liep: de dialoog is die vraag zelf, niet een kopie ervan.
  pendingSwitch = $state('')
  switchInFlight = $state(false)
  deviceCheck = $state<'idle' | 'checking' | 'ready' | 'refused' | 'unavailable'>('idle')
  deviceCheckHeard = $state(false)
  deviceCheckAudio = $state(false)
  partial = $state<{ speaker: string; text: string } | null>(null)
  answers = $state.raw<AnswerLine[]>([])
  // De laatste bijdrage van de duif die net van tafel ging. Zij hoort
  // niet in `answers` thuis — dat is de lijst van deze sessie — maar het
  // scherm mag haar niet zomaar leegvegen: dan is de vorige duif ineens nooit
  // aan tafel geweest. Zij blijft staan, met haar naam eraan, tot de nieuwe
  // duif haar eerste bijdrage levert.
  carried = $state.raw<AnswerLine | null>(null)
  // Compatibility projection of the last turn. Each AnswerLine also owns its
  // immutable source snapshot for current-session history.
  sources = $state.raw<Source[]>([])
  kbStatus = $state<KBStatus | undefined>(undefined)
  // sourceDrops is de bronuitval van de lopende beurt; hij reist met
  // de bronnen mee naar de bijdrage waar hij bij hoort. De naam staat los van
  // `dropped` hierboven: dat is het aantal gemiste kanaalberichten, iets heel
  // anders.
  sourceDrops = $state<SourceDrops | undefined>(undefined)

  // --- kandidaat, ingrijpen en de meter --------------------------------------
  arousal = $state(0)
  // candidate is wat de duif nu klaar heeft liggen. Null betekent: niets
  // — en dat is de normale stand.
  candidate = $state<MeetingCandidate | null>(null)
  candidateInFlight = $state(false)
  trigger = $state<{ topic: TopicRef | null; addressed: boolean } | null>(null)
  detector = $state<(NonNullable<MeetingEvent['detector']> & { message?: Bilingual }) | null>(null)
  detectorRetryInFlight = $state(false)
  // presence is de openstaande vraag "Ben je er nog?", met de uitleg van de
  // server. presenceDeadline is het moment op de klok van de browser
  // (ports.now) waarop de aftelling op nul staat, 0 zonder aftelling;
  // presenceNow is de laatst getekende tel. De resterende tijd is het verschil,
  // nooit een teller die zelf aftelt.
  presence = $state<Bilingual | null>(null)
  presenceDeadline = $state(0)
  presenceNow = $state(0)

  // --- meldingen ------------------------------------------------------------
  // busy is het bezetbericht van de server (bij een tweede druk): net als error een
  // tweetalige melding die pas bij het lezen in de UI-taal wordt gezet.
  busy = $state<ApiFailure | null>(null)
  error = $state<ApiFailure | null>(null)
  errorFallback = $state<MsgKey>('error.generic')
  audioBlocked = $state(false)
  audioGap = $state(false)
  // voiceUnavailable: de stem van deze beurt begon nooit (tts_unavailable). Dat
  // is geen gat in het geluid — er speelde niets — dus het krijgt zijn eigen
  // melding in plaats van "het geluid speelde niet volledig af".
  voiceUnavailable = $state(false)
  // De microfoonmelding staat los van de foutmelding: een latere serverfout
  // mag niet wegpoetsen dat de duif helemaal niet meeluistert. micFailure is
  // de servervariant ervan (geen STT, een 5xx op /audio).
  micNotice = $state<MsgKey | ''>('')
  micFailure = $state<ApiFailure | null>(null)

  // --- afgeleiden -----------------------------------------------------------
  // De indicator is een afgeleide van de lokale microfoon × de serverstand,
  // nooit een toewijzing vanuit één event.
  listen: ListenState = $derived(
    this.localMic === 'open' && this.serverListen !== LISTEN.off ? this.serverListen : LISTEN.off,
  )
  dropped = $derived(this.droppedLocal + this.droppedServer)
  meetingPigeons = $derived(this.pigeons.filter((p) => p.meeting))
  pigeon = $derived(this.pigeons.find((p) => p.id === this.pigeonId) ?? null)
  // Een pratende duif krijgt de antwoordknop; de non-verbale duif haar
  // eigen ingrijpknop. De stopknop houden ze allebei.
  speaks = $derived(this.pigeon === null || this.pigeon.chat !== false)
  // Deze drie zijn getters en geen $derived: ze lezen de locale, en een
  // veldinitialisator draait vóór de constructor die haar toewijst. Wat erin
  // staat is $state, dus ze zijn even reactief.
  get errorText(): string {
    return this.error ? this.locale.fromServer(this.error.message, this.error.reason, this.errorFallback) : ''
  }

  get busyText(): string {
    return this.busy ? this.locale.fromServer(this.busy.message, this.busy.reason) : ''
  }

  get micNoticeText(): string {
    if (this.micFailure) {
      return this.locale.fromServer(this.micFailure.message, this.micFailure.reason, 'common.connection.lost')
    }
    return this.micNotice ? this.locale.t(this.micNotice) : ''
  }

  // notice is het hele statusgebied: hoogstens één melding, en de volgorde ligt
  // hier vast in plaats van in de volgorde van tien {#if}-blokken. Een fout gaat
  // voor, daarna wat de gebruiker zelf kan verhelpen (geluid, microfoon), daarna
  // de rest. Wat de gebruiker niet kan verhelpen — weggevallen audioblokken, een
  // kostenlimiet, een onbruikbaar detectieresultaat — staat in de console, niet
  // op het scherm; de gevolgen die zij wél merkt staan er in gewone taal.
  get notice(): MeetingNotice | null {
    const error = this.errorText
    if (error) return { id: 'error', tone: 'danger', text: error }
    if (this.audioBlocked) {
      return {
        id: 'sound-blocked',
        tone: 'warning',
        text: this.locale.t(this.speaks ? 'meeting.audio.blocked' : 'meeting.signal.audioBlocked'),
        action: 'sound',
      }
    }
    const mic = this.micNoticeText
    if (mic) return { id: 'microphone', tone: 'warning', text: mic, action: 'microphone' }
    if (this.localMic === 'opening') return { id: 'permission', tone: 'secondary', text: this.locale.t('meeting.permission') }
    const busy = this.busyText
    if (busy) return { id: 'busy', tone: 'warning', text: busy }
    if (this.stopSyncFailed) {
      return { id: 'stop-sync', tone: 'warning', text: this.locale.t('meeting.notice.stopFailed') }
    }
    if (this.speaks && this.voiceUnavailable && !this.audioGap) {
      return { id: 'voice-unavailable', tone: 'warning', text: this.locale.t('meeting.notice.voiceUnavailable') }
    }
    if (this.speaks && (this.audioGap || this.playbackSyncFailed)) {
      return { id: 'sound', tone: 'warning', text: this.locale.t('meeting.notice.sound') }
    }
    if (!this.speaks && this.signalError) {
      return { id: 'signal', tone: 'warning', text: this.locale.t('meeting.signal.audioFailed') }
    }
    // De detectie gaat voor de stem: dat de duif geen stem heeft weet de
    // begeleider al bij het opzetten, dat zij uit zichzelf niets meer inbrengt
    // niet.
    if (this.settings.autonomous && this.detector?.enabled === false) {
      return { id: 'detector', tone: 'secondary', text: this.locale.t('meeting.detector.paused') }
    }
    if (this.speaks && this.voice === VOICE.off) {
      return { id: 'voice', tone: 'secondary', text: this.locale.t('meeting.voice.off') }
    }
    return null
  }

  // Of de knop van de inbreng op "hervatten" mag staan. De duif is hier stil
  // zodra je op "Stop inbreng" drukt (autonomyPaused), maar hervatten heeft
  // alleen zin als de server die stop ook kent. Blijft de stop onderweg
  // (stopPending) of bereikte hij de server niet (stopSyncFailed), dan houdt de
  // knop zijn stopstand: de storingsmelding vraagt dan juist om nog een keer
  // stoppen, en een knop die op dat moment een hervat-verzoek stuurt zou de
  // duif weer aan het woord laten.
  get contributionResumable(): boolean {
    return this.autonomyPaused && !this.stopPending && !this.stopSyncFailed
  }

  // De vergaderstand in vier woorden. Alles wat het statusgebied als fout of
  // waarschuwing toont is hier één stand: er is iets aan de hand.
  get status(): MeetingStatus {
    const notice = this.notice
    if (notice && notice.tone !== 'secondary') return 'attention'
    if (this.speaking) return 'speaking'
    if (this.respondInFlight) return 'thinking'
    if (this.autonomyPaused || this.stopped) return 'paused'
    return this.listen === LISTEN.listening ? 'listening' : 'paused'
  }

  get closedText(): string {
    return this.renderClosed(this.closedReason)
  }

  // presenceRemainingMs is de aftelling van de open vraag, of null als er geen
  // vraag openstaat of de server geen tijd meegaf.
  get presenceRemainingMs(): number | null {
    if (!this.presence || this.presenceDeadline === 0) return null
    return Math.max(0, this.presenceDeadline - this.presenceNow)
  }

  // stuck is waar: de vergadering die de plaats bezet is er één van dit
  // account, dus de gebruiker kan zichzelf losmaken. Bij een tafel die door
  // anderen vol zit blijft de melding staan zonder knop — daar valt niets te
  // bevrijden.
  get stuck(): boolean {
    return this.phase === 'setup' && this.error?.reason === CAPACITY_SELF
  }

  // --- niet-reactieve binnenkant --------------------------------------------
  // sessionGen ongeldigt élke lopende async-voortzetting: een laat antwoord van
  // een vorige sessie raakt de nieuwe nooit.
  private sessionGen = 0
  private audioGeneration = 0
  // micGeneration staat los van de sessie: bij een duifwissel gaat de sessie
  // over, maar de microfoon blijft dezelfde open bron. Alleen closeMic
  // en het neerleggen van de vergadering ongeldigen hem.
  private micGeneration = 0
  // A local Stop is released only by a confirmed explicit resume. Server
  // snapshots cannot undo that intention when the stop request was lost.
  private localPauseHeld = false
  private pauseRevision = 0
  private resolvedStopThrough = -1n
  private lastStopBarrier = -1n
  private settingsRevision = 0
  private ctx: AudioContextLike | null = null
  private channel: EventSourceLike | null = null
  // channelDown: het kanaal viel weg en de browser probeert het opnieuw. Tot er
  // weer een bericht binnenkomt (de server opent elke verbinding met zijn
  // stand) kan er niets binnenkomen, hoe goed het audio ook aankomt.
  private channelDown = false
  private mic: MicLike | null = null
  private sluice: AudioSluice
  private readonly collector = new TtsCollector()
  // De streamende kant van dezelfde beurt; turnFormat onthoudt welke van de
  // twee deze beurt gebruikt, want tts_done draagt het formaat niet.
  private readonly pcm = new PcmTurn()
  private synthesisFailed = false
  private turnFormat: string | null = null
  private readonly player: TtsPlayer
  private readonly sounds = new SoundBoard()
  private acceptTts = false
  private answerOpen = false
  private transcriptSeq = 0
  private answerSeq = 0
  private bound = false
  private loadingPigeons: Promise<void> | null = null
  private configurationGeneration = 0
  private settingsChosen = false
  // De gedeelde stand van zelfstandig meedoen uit `gedrag`: de terugval voor
  // een duif die er zelf niets over zegt.
  private sharedAutonomy = false
  // autonomyChosen zegt dat de bezoeker de schakelaar zelf heeft aangeraakt.
  // Vanaf dat moment is de stand een keuze over deze vergadering en niet meer
  // de standaard van een duif, dus laat elke duifwissel hem staan.
  private autonomyChosen = false
  private settingsMayResume = false
  private cancelSignal: (() => void) | null = null
  private readonly seenSignals = new Set<string>()
  private legacySignalSequence = 0
  private generationComplete = false
  private handlingPlaybackFailure = false
  private turnAudioReceived = false
  private readonly playbackAcks = new Set<string>()
  private highestTurn = 0n
  private observedControlTurn = 0n
  private signalPlaybackId = ''
  private cancelSignalWatchdog: (() => void) | null = null
  private manualTurnPending = false
  private manualTurnActive = false
  private deviceCheckGeneration = 0
  private checkContext: AudioContextLike | null = null
  private checkMic: MicLike | null = null
  private cancelDeviceCheck: (() => void) | null = null
  // De uitgestelde /settings-POST en zijn annulering; settingsInFlight telt de
  // POSTs die nog onderweg zijn.
  private cancelSettings: (() => void) | null = null
  private settingsInFlight = 0
  // De aftelling van "Ben je er nog?": het hertekenen en de lokale terugval.
  private cancelPresenceRedraw: (() => void) | null = null
  private cancelPresenceFallback: (() => void) | null = null
  // De uitleg van de laatste vraag, om haar terug te zetten als een hartslag
  // zegt dat zij op de server nog openstaat; presenceAnswering zolang er een
  // "ja" onderweg is — dan mag een oudere hartslag haar niet terugzetten.
  private presenceMessage: Bilingual = {}
  private presenceAnswering = false
  // De drossel van het interactiesignaal: zolang hij loopt gaat er geen nieuw
  // signaal weg, en interactionPending onthoudt dat er intussen iets gebeurde.
  private cancelInteractionHold: (() => void) | null = null
  private interactionPending = false

  constructor(
    readonly ports: MeetingPorts,
    readonly locale: UiLocale,
  ) {
    this.sluice = this.createSluice()
    this.player = new TtsPlayer(
      () => this.ctx,
      () => {
        this.audioGap = true
      },
      (state) => this.onPlayback(state),
    )
  }

  // --- opzet ----------------------------------------------------------------

  loadPigeons(): Promise<void> {
    if (this.pigeonsLoaded && !this.pigeonsFailed) return Promise.resolve()
    if (this.loadingPigeons) return this.loadingPigeons
    const generation = this.configurationGeneration
    this.loadingPigeons = (async () => {
      const result = await fetchPigeons(this.ports.fetch)
      if (generation !== this.configurationGeneration) return
      if (!result.ok) {
        this.pigeonsFailed = true
        this.pigeonsLoaded = true
        return
      }
      this.pigeonsFailed = false
      this.pigeons = result.pigeons
      this.pigeonsLoaded = true
      this.applyBehaviour(result.behaviour)
      if (!this.pigeonId) this.pigeonId = this.meetingPigeons[0]?.id ?? ''
      // De duif kan al gekozen zijn voordat deze lijst binnenkwam (de schil
      // zet hem), en dan is dit het eerste moment waarop haar eigen
      // standaardstand bekend is.
      this.applyPigeonDefaults()
    })().finally(() => { this.loadingPigeons = null })
    return this.loadingPigeons
  }

  // applyBehaviour zet de startwaarden van de opzetkaart uit de gedragsconfig;
  // de server blijft de baas, dit is alleen wat er in de velden staat.
  private applyBehaviour(behaviour: BehaviourDTO | undefined): void {
    if (!behaviour) return
    // De gedeelde stand blijft de terugval voor een duif die zelf niets zegt;
    // hij wordt onthouden omdat de duif ná deze lijst nog kan wisselen.
    this.sharedAutonomy = Boolean(behaviour.interventieZelfstandig)
    if (this.phase !== 'setup' || this.settingsChosen) return
    this.settings = {
      autonomous: this.autonomyDefaultFor(this.pigeonId),
      eagerness: isEagerness(behaviour.detectieGretigheid) ? behaviour.detectieGretigheid : 'normal',
      cooldownSeconds: clampCooldown(behaviour.interventieThrottleSeconden),
    }
    // De moduletaal begint op de configuratietaal, maar alleen zolang de
    // gebruiker zelf nog niets koos. "Nog geen keuze" is een eigen
    // vlag, geen gok op basis van de huidige waarde: wie bewust Engels kiest
    // hoort niet alsnog naar een Nederlandse default te schuiven.
    if (isLang(behaviour.taalDefault) && !this.locale.chosen) this.locale.lang = behaviour.taalDefault
  }

  // autonomyDefaultFor geeft de startstand van zelfstandig meedoen voor één
  // duif: haar eigen stand uit de personalijst, of de gedeelde stand zolang de
  // lijst er nog niet is.
  autonomyDefaultFor(pigeonId: string): boolean {
    const pigeon = this.pigeons.find((p) => p.id === pigeonId)
    return pigeon ? pigeon.autonomousDefault : this.sharedAutonomy
  }

  // applyPigeonDefaults zet de opzetkaart op de standaarden van de gekozen
  // duif. Alleen vóór de vergadering, en alleen zolang de gebruiker de
  // schakelaar niet zelf heeft aangeraakt: wie bewust iets kiest hoort dat niet
  // te verliezen door even een andere duif te bekijken.
  private applyPigeonDefaults(): void {
    if (this.phase !== 'setup' || this.autonomyChosen) return
    const autonomous = this.autonomyDefaultFor(this.pigeonIdState)
    if (autonomous === this.settings.autonomous) return
    this.settings = { ...this.settings, autonomous }
  }

  // --- de vergadering openen ------------------------------------------------

  // start opent een vergadering vanaf het opzetscherm. `replaceExisting` is de
  // uitweg uit de capaciteitsmelding: alleen na een uitdrukkelijk "ja" van de
  // gebruiker, en alleen voor haar eigen open vergadering.
  async start(replaceExisting = false): Promise<void> {
    // Dubbelklik-guard: twee keer klikken opent geen twee vergaderingen.
    if (this.phase !== 'setup') return
    this.cancelDeviceTest()
    this.phase = 'opening'
    this.bindPageHandlers()
    this.resetSession()
    const gen = ++this.sessionGen
    // De AudioContext ontstaat synchroon in de klik: een context die pas na een
    // await wordt gemaakt telt voor de browser niet meer als gebruikersgebaar.
    this.ctx = this.createContext()
    void this.unblockAudio(gen)
    this.sluice = this.createSluice()

    let res: Response
    try {
      res = await this.ports.fetch(API + ROUTES.sessions, jsonInit(this.openBody(replaceExisting ? { replaceExisting: true } : {})))
    } catch {
      if (gen !== this.sessionGen) return
      this.failWith('meeting.start.failed', { status: 0 })
      this.abortOpening()
      return
    }
    if (gen !== this.sessionGen) {
      // Deze sessie is al ingehaald (unmount of een tweede opening): netjes
      // opruimen zodat er geen weesje op de server achterblijft.
      await this.discard(res)
      return
    }
    if (!res.ok) {
      await this.failFrom(res, 'meeting.start.failed', gen)
      if (gen === this.sessionGen) this.abortOpening()
      return
    }
    const data = (await res.json().catch(() => null)) as OpenResponse | null
    // De sessie kan tijdens het lezen van de body al zijn ingehaald; pas daarna
    // mag er iets over "mislukt" op het scherm komen.
    if (gen !== this.sessionGen) {
      if (data?.sessionId) this.deleteSession(data.sessionId)
      return
    }
    if (!data?.sessionId) {
      this.failWith('meeting.start.failed', { status: res.status })
      this.abortOpening()
      return
    }
    this.sessionId = data.sessionId
    this.pigeonId = data.pigeonId || this.pigeonId
    this.lang = isLang(data.lang) ? data.lang : 'en'
    this.voice = isVoiceState(data.voice) ? data.voice : VOICE.off
    this.serverListen = isListenState(data.listening) ? data.listening : LISTEN.off
    // De open-respons draagt de gedragsknoppen sinds kort in `settings`; de
    // losse velden blijven de terugval voor een oudere server.
    this.applySettings(data.settings ?? { autonomous: data.autonomous, eagerness: data.eagerness })
    this.autonomyPaused = data.autonomyPaused === true
    this.playbackTracking = data.playbackTracking === true
    this.phase = 'live'
    this.live = true
    this.disposed = false
    this.bindPageHandlers()
    this.openChannel(gen)
    await this.openMic(gen)
  }

  // openBody is het verzoek waarmee een duif aan tafel gaat. `pigeon` is de duif
  // die gaat zitten en hoeft alleen meegegeven te worden waar dat een andere is
  // dan de gekozen duif — bij een wissel, want dat is een nieuwe
  // vergadering met een nieuwe duif. Zelfstandig meedoen hoort dan ook haar
  // stand te zijn en niet die van haar voorganger: anders erft de signalerende
  // duif het zwijgen van de stakeholder en zwijgt zij de hele vergadering.
  // Raakte de bezoeker de
  // schakelaar zelf aan, dan is dat een keuze over de vergadering en reist zij
  // ongemoeid mee.
  private openBody(extra: Record<string, unknown> = {}, pigeon: string = this.pigeonId): Record<string, unknown> {
    return {
      pigeonId: pigeon,
      playbackTracking: true,
      lang: this.locale.lang,
      autonomous: this.autonomyChosen ? this.settings.autonomous : this.autonomyDefaultFor(pigeon),
      eagerness: this.settings.eagerness,
      cooldownSeconds: this.settings.cooldownSeconds,
      ...extra,
    }
  }

  private createContext(): AudioContextLike | null {
    let ctx: AudioContextLike
    try {
      ctx = this.ports.createAudioContext()
    } catch {
      return null
    }
    // De blokkademelding is een afgeleide van de contextstand: één keer meteen
    // (de stand vlak na het openen) en daarna bij elke wissel.
    this.audioBlocked = ctx.state === 'suspended'
    ctx.onStateChange((state) => {
      if (this.ctx !== ctx) return
      this.audioBlocked = state === 'suspended'
      if (this.audioBlocked && this.speaking) {
        this.silenceLocal()
        this.onPlayback('failed')
        if (this.speaks) this.audioGap = true
        else this.signalError = true
      }
    })
    return ctx
  }

  // unblockAudio wekt de context; blijft hij geschorst, dan zegt het scherm dat
  // met een knop erbij in plaats van stil geen geluid te maken.
  private async unblockAudio(gen: number): Promise<void> {
    const ctx = this.ctx
    if (!ctx) return
    try {
      await ctx.resume()
    } catch {
      // een geweigerde resume is precies de blokkade die we melden
    }
    if (gen !== this.sessionGen || this.ctx !== ctx) return
    this.audioBlocked = ctx.state === 'suspended'
  }

  // enableAudio hoort bij de knop onder de blokkademelding: die klik is wél een
  // gebruikersgebaar.
  async enableAudio(): Promise<void> {
    await this.unblockAudio(this.sessionGen)
  }

  private abortOpening(): void {
    const ctx = this.ctx
    this.ctx = null
    if (ctx) void ctx.close().catch(() => {})
    this.sluice.close()
    this.phase = 'setup'
    this.live = false
    this.unbindPageHandlers()
  }

  private async discard(res: Response): Promise<void> {
    const data = (await res.json().catch(() => null)) as OpenResponse | null
    if (data?.sessionId) this.deleteSession(data.sessionId)
  }

  // --- het sessiekanaal -----------------------------------------------------

  private openChannel(gen: number): void {
    const url = this.sessionUrl(this.sessionId) + ROUTES.events
    const channel = this.ports.openEvents(url)
    this.channel = channel
    this.channelDown = false
    channel.onMessage((data) => {
      if (gen !== this.sessionGen) return
      this.channelDown = false
      let ev: MeetingEvent | null = null
      try {
        ev = JSON.parse(data) as MeetingEvent
      } catch {
        return // een kapot frame mag de stroom niet breken
      }
      if (ev && typeof ev.type === 'string') this.onEvent(ev)
    })
    channel.onError((terminal) => {
      if (gen !== this.sessionGen) return
      if (terminal) {
        // Definitief dicht: de browser probeert niets meer, dus de vergadering
        // is voor ons afgelopen. Liever eerlijk sluiten dan groen blijven.
        this.layDown(CLOSE_CONNECTION_LOST, true)
        return
      }
      // Een weggevallen kanaal is geen groen lampje: liever vals-negatief dan
      // vals-positief. De browser probeert zelf opnieuw.
      this.channelDown = true
      this.serverListen = LISTEN.reconnecting
    })
  }

  onEvent(ev: MeetingEvent): void {
    if (ev.sessionId && ev.sessionId !== this.sessionId) return
    if (ev.turnId && ev.type !== EVENTS.persona && ev.type !== EVENTS.sound && ev.type !== EVENTS.state && ev.type !== EVENTS.stopped && ev.turnId !== this.activeTurnId) return
    if (ev.settings) this.observeControlSettings(ev.settings)
    switch (ev.type) {
      case EVENTS.state:
        this.onState(ev)
        break
      case EVENTS.listeningHeartbeat:
        // Geen resterende tijd op de hartslag: er staat op de server geen vraag
        // (meer) open, want iemand antwoordde in een ander tabblad of klikte.
        // Openen doet de hartslag nooit — dat is aan presence_check en state.
        this.followPresence(ev.graceMs)
        this.serverListen = isListenState(ev.listening) ? ev.listening : LISTEN.off
        // Tussen de stopknop en het `stopped`-bericht mag een hartslag de duif
        // niet opnieuw aan het woord verklaren.
        if (!((this.synthesisFailed || this.stopPending || (this.autonomyPaused && !this.manualTurnActive)) && ev.speaking)) this.speaking = Boolean(ev.speaking)
        if (!this.stopPending) this.applyAutonomyPause(ev.autonomyPaused)
        break
      case EVENTS.transcript:
        this.onTranscript(ev)
        break
      case EVENTS.persona:
        if (this.autonomyPaused && !this.manualTurnPending) return
        if (ev.turnId && !this.beginTurn(ev.turnId)) return
        this.manualTurnActive = this.manualTurnPending
        this.manualTurnPending = false
        this.generationComplete = false
        this.turnAudioReceived = false
        this.synthesisFailed = false
        this.playbackStatus = 'pending'
        if (isLang(ev.lang)) this.lang = ev.lang
        if (ev.pigeonId) this.pigeonId = ev.pigeonId
        this.stopped = false
        this.stopPending = false
        this.speaking = true
        this.busy = null
        this.audioGap = false
        this.voiceUnavailable = false
        // Een nieuwe beurt begint zonder bronnen: die van de vorige horen niet
        // bij dit antwoord.
        this.sources = []
        this.kbStatus = undefined
        this.sourceDrops = undefined
        this.collector.reset()
        this.pcm.reset()
        this.turnFormat = null
        this.acceptTts = true
        this.openAnswer('')
        break
      case EVENTS.text:
        this.appendAnswer(ev.delta ?? '')
        break
      case EVENTS.sources:
        // De bronnen van deze beurt, nog vóór de eerste zin af is. Zonder dit
        // bleef een "[1]" ruwe tekst tot de duif was uitgesproken.
        if (!this.answerOpen) return
        this.sources = parseSources(ev.sources)
        this.kbStatus = parseKBStatus(ev.kbStatus)
        this.sourceDrops = parseSourceDrops(ev.dropped)
        this.updateLastAnswer({ sources: this.sources, kbStatus: this.kbStatus, dropped: this.sourceDrops })
        break
      case EVENTS.textDone:
        // De tekst is compleet; de stem speelt mogelijk nog. De kaart sluit
        // hier af (cursor weg, status naar 'spreekt'), de beurt zelf niet: die
        // blijft open tot response_done, zodat de laatste audioframes en de
        // stopreden nog binnen mogen komen.
        if (!this.answerOpen) return
        if (ev.sources !== undefined) {
          this.sources = parseSources(ev.sources)
          this.kbStatus = parseKBStatus(ev.kbStatus)
        }
        this.updateLastAnswer({
          sources: this.sources, kbStatus: this.kbStatus, dropped: this.sourceDrops, status: 'complete',
          stopReason: ev.stopReason === 'truncated' || ev.stopReason === 'filtered' ? ev.stopReason : undefined,
        })
        break
      case EVENTS.ttsAudio:
        this.onTtsAudio(ev)
        break
      case EVENTS.ttsDone:
        this.onTtsDone(ev)
        break
      case EVENTS.responseDone:
        if (!this.answerOpen) return
        this.generationComplete = true
        this.sources = parseSources(ev.sources)
        this.kbStatus = parseKBStatus(ev.kbStatus)
        this.updateLastAnswer({
          sources: this.sources, kbStatus: this.kbStatus, dropped: this.sourceDrops, status: 'complete',
          stopReason: ev.stopReason === 'truncated' || ev.stopReason === 'filtered' ? ev.stopReason : undefined,
        })
        this.manualTurnActive = false
        if (!this.playbackTracking || ['ended', 'failed', 'aborted'].includes(this.playbackStatus)) this.speaking = false
        else if (!this.turnAudioReceived) this.onPlayback(this.voice === VOICE.off ? 'ended' : 'failed')
        this.player.finish()
        this.acceptTts = false
        this.answerOpen = false
        this.collector.reset()
        this.pcm.reset()
        this.turnFormat = null
        break
      case EVENTS.busy:
        // Drukt de gebruiker zelf op de knop, dan is het HTTP-antwoord de
        // terugkoppeling; het SSE-bericht zou daar dubbelop overheen komen.
        if (this.respondInFlight) return
        this.busy = { status: 409, reason: ev.error, message: ev.message }
        break
      case EVENTS.stopped:
        // stopped is a session control barrier carrying the *new* server
        // generation; it is not a frame of the canceled turn.
        if (ev.turnId && !this.acceptStopBarrier(ev.turnId)) return
        // A superseded, still-inaudible automatic turn is interrupted, not a
        // user Stop. It must not lock future relevant turns or clear an
        // independently held local Stop/failure pause.
        const contextChanged = ev.reason === 'context_changed' && ev.autonomyPaused === false
        // De stopbarrière van de server is het bewijs dat de bediening wél
        // aankwam; een eerdere melding daarover heeft afgedaan.
        if (!contextChanged) this.stopSyncFailed = false
        this.silenceLocal()
        if (!contextChanged) this.holdLocalPause(false)
        this.activeTurnId = ''
        this.stopped = this.localPauseHeld || !contextChanged
        this.applyAutonomyPause(!contextChanged)
        this.manualTurnActive = false
        this.updateLastAnswer({ status: contextChanged ? 'interrupted' : 'stopped' })
        this.answerOpen = false
        this.stopPending = false
        this.speaking = false
        if (isListenState(ev.listening)) this.serverListen = ev.listening
        break
      case EVENTS.playbackState:
        if (ev.playback === 'timeout' || ev.playback === 'failed' || ev.playback === 'aborted') {
          this.silenceLocal()
          this.playbackStatus = ev.playback === 'timeout' ? 'failed' : ev.playback
          if (ev.playback === 'timeout') this.playbackSyncFailed = true
          this.autonomyPaused = true
        }
        if (typeof ev.speaking === 'boolean' && (!this.synthesisFailed || !ev.speaking)) this.speaking = ev.speaking
        this.applyAutonomyPause(ev.autonomyPaused)
        break
      case EVENTS.error:
        this.generationComplete = true
        this.silenceLocal()
        // Alleen bevestigen wat er werkelijk klonk: kwam er geen audio voor
        // deze beurt, dan blijft het bij de lokale stand.
        // De beurtklok is geen afspeelfout: dan geen lokale pauze.
        this.onPlayback('failed', this.turnAudioReceived, ev.error !== TURN_TIMEOUT)
        this.updateLastAnswer({ status: 'interrupted' })
        this.manualTurnActive = false
        this.manualTurnPending = false
        this.failWith('error.generic', { status: 0, message: ev.message, reason: ev.error })
        this.speaking = false
        this.acceptTts = false
        this.answerOpen = false
        this.collector.reset()
        this.pcm.reset()
        this.turnFormat = null
        break
      case EVENTS.presenceCheck:
        this.openPresence(ev.message, ev.graceMs)
        break
      case EVENTS.sessionClosed:
        // Eerst het kanaal dicht, dan pas de toestand bijwerken: anders kan een
        // nagekomen bericht de zojuist opgeruimde sessie weer aanzetten.
        this.channel?.close()
        this.channel = null
        this.collector.reset()
        this.pcm.reset()
        this.turnFormat = null
        this.layDown(ev.reason ?? '', false)
        break
      case EVENTS.candidate:
        // Een kandidaat vervangt zijn voorganger; een leeg bericht wist hem.
        // De duif kan tussen twee evaluaties van gedachten veranderen, en dan
        // hoort het blok gewoon te verdwijnen.
        this.candidate = parseCandidate(ev.candidate)
        break
      case EVENTS.arousal:
        this.arousal = typeof ev.arousal === 'number' ? Math.max(0, Math.min(1, ev.arousal)) : 0
        break
      case EVENTS.intervention:
        this.trigger = { topic: ev.topic ?? null, addressed: ev.addressed === true }
        this.candidate = null
        break
      case EVENTS.detectorStatus:
        this.detector = { ...ev.detector, enabled: ev.detector?.enabled !== false, message: ev.message }
        break
      case EVENTS.sound:
        this.onSound(ev)
        break
      case EVENTS.settings:
        // Een echo van onze eigen wijziging mag de knoppen niet terugzetten
        // terwijl de gebruiker nog aan het schuiven is; de server wint pas als
        // er niets meer van ons onderweg is.
        if (this.settingsPending()) break
        this.applySettings(ev.settings)
        this.applyAutonomyPause(ev.autonomyPaused)
        break
      default:
        // Onbekende berichttypes worden genegeerd; het protocol mag groeien.
        break
    }
  }

  // onState is óók het herstelbericht na een herverbinding: de per-beurt-stand
  // gaat op nul en een lopende uiting krijgt een eerlijk gemerkt antwoordblok
  // (de tekst die we misten komt niet meer).
  private onState(ev: MeetingEvent): void {
    // Reconnecting cannot recover missing audio frames. Stop every old local
    // player, even if the server already reports speaking:false.
    this.silenceLocal()
    if (ev.turnId) {
      const ordinal = this.turnOrdinal(ev.turnId)
      if (ordinal !== null) {
        this.highestTurn = this.highestTurn > ordinal ? this.highestTurn : ordinal
        this.observedControlTurn = this.observedControlTurn > ordinal ? this.observedControlTurn : ordinal
      }
    }
    if (isLang(ev.lang)) this.lang = ev.lang
    if (ev.pigeonId) this.pigeonId = ev.pigeonId
    if (isVoiceState(ev.voice)) this.voice = ev.voice
    // Na een herverbinding staat de vraag er weer, met de tijd die de server
    // nog geeft; staat er op de server geen vraag, dan hier ook niet.
    if (typeof ev.graceMs === 'number' && ev.graceMs > 0) this.openPresence(ev.message, ev.graceMs)
    else this.closePresence()
    if (typeof ev.playbackTracking === 'boolean') this.playbackTracking = ev.playbackTracking
    if (ev.detector) this.detector = { ...ev.detector }
    this.serverListen = isListenState(ev.listening) ? ev.listening : LISTEN.off
    if (!this.settingsPending()) this.applySettings(ev.settings)
    if (!this.stopPending) this.applyAutonomyPause(ev.autonomyPaused)
    this.partial = null
    this.stopPending = false
    this.collector.reset()
    this.pcm.reset()
    this.turnFormat = null
    this.updateLastAnswer({ status: 'interrupted' })
    // Frames van de lopende beurt horen bij een uiting waarvan we het begin
    // missen: negeren tot de volgende persona.
    this.acceptTts = false
    this.answerOpen = false
    if (ev.speaking && this.playbackTracking && ev.turnId) {
      this.silenceLocal()
      this.activeTurnId = ev.turnId
      this.onPlayback('failed')
      this.autonomyPaused = true
      this.speaking = false
    } else if (ev.speaking && !this.autonomyPaused) {
      this.speaking = true
      this.openAnswer(this.locale.t('meeting.answers.reconnected'))
    } else {
      this.speaking = false
    }
  }

  private onTranscript(ev: MeetingEvent): void {
    if (!ev.final) {
      if (this.listen !== LISTEN.listening) { this.partial = null; return }
      this.partial = { speaker: ev.speaker ?? '', text: ev.text ?? '' }
      return
    }
    this.partial = null
    // Een gehoorde zin beantwoordt "Ben je er nog?" niet: de microfoon hoort ook
    // de zaal en de duif zelf. Alleen een klik of toets doet dat.
    const line: TranscriptLine = {
      seq: ++this.transcriptSeq,
      speaker: ev.speaker ?? '',
      text: ev.text ?? '',
      own: Boolean(ev.pigeonWindow),
    }
    const kept = this.transcript.length >= MAX_TRANSCRIPT ? this.transcript.slice(this.transcript.length - MAX_TRANSCRIPT + 1) : this.transcript.slice()
    kept.push(line)
    this.transcript = kept
  }

  // onTtsAudio: PCM16 klinkt meteen (latentie boven compactheid); MP3 wacht
  // in de verzamelaar op tts_done, want losse MP3-chunks decoderen niet.
  private onTtsAudio(ev: MeetingEvent): void {
    if (!this.acceptTts || this.stopped) return
    this.turnAudioReceived = true
    if (ev.format !== AUDIO_FORMATS.pcm16) {
      this.collector.add(ev.audio, ev.seq)
      return
    }
    this.turnFormat = AUDIO_FORMATS.pcm16
    const samples = this.pcm.add(ev.audio, ev.seq)
    if (!samples) return
    if (this.audioBlocked) { this.onPlayback('failed'); return }
    this.player.write(samples, ev.sampleRate ?? DEFAULT_PCM_RATE)
  }

  private onTtsDone(ev: MeetingEvent): void {
    if (ev.error) {
      if (this.stopped || !this.acceptTts) return
      this.synthesisFailed = true
      // Begon de stem nooit, dan is er geen gat te melden maar een stem die
      // ontbreekt; wat er al klonk en daarna stokte blijft een gat.
      if (ev.error === TTS_UNAVAILABLE && !this.turnAudioReceived) this.voiceUnavailable = true
      else this.audioGap = true
      // A synthesis failure is not a failure of the still-streaming answer.
      // Stop audio immediately and preserve the answer. The server already knows
      // the failure and will release/pause after text completion; no ACK cancels it.
      this.handlingPlaybackFailure = true
      try { this.silenceLocal() } finally { this.handlingPlaybackFailure = false }
      this.acceptTts = false
      this.playbackStatus = 'failed'
      this.speaking = false
      this.holdLocalPause()
      return
    }
    if (this.turnFormat === AUDIO_FORMATS.pcm16) {
      // De audio klonk al; hier rest alleen de eerlijkheid over gaten.
      const verdict = this.pcm.finish(ev.frames)
      this.turnFormat = null
      if (this.stopped || !this.acceptTts) return
      if (!verdict.ok && (verdict.reason !== 'empty' || this.voice === VOICE.ready)) { this.audioGap = true; this.onPlayback('failed') }
      this.player.finish()
      return
    }
    const result = this.collector.finish(ev.frames)
    if (this.stopped || !this.acceptTts) return
    if (!result.ok) {
      // Nul frames terwijl de sessie een stem heeft, een gat in de nummering of
      // een te grote beurt: eerlijk melden, nooit een halve MP3 afspelen.
      if (result.reason !== 'empty' || this.voice === VOICE.ready) { this.audioGap = true; this.onPlayback('failed') }
      return
    }
    if (this.audioBlocked) {
      // De browser houdt geluid tegen: dan hoort er ook niets te wachten. De
      // melding staat al op het scherm; de tekst is compleet.
      this.player.silence()
      this.onPlayback('failed')
      return
    }
    void this.player.enqueue(result.bytes)
    this.player.finish()
  }

  private openAnswer(note: string): void {
    // De nieuwe duif neemt het woord: de kaart van haar voorganger heeft haar
    // werk gedaan.
    this.carried = null
    const line: AnswerLine = { id: ++this.answerSeq, text: '', note, pigeonId: this.pigeonId, turnId: this.activeTurnId, afterSeq: this.transcriptSeq, sources: [], status: 'streaming', startedAt: Date.now() }
    const kept = this.answers.length >= MAX_ANSWERS ? this.answers.slice(this.answers.length - MAX_ANSWERS + 1) : this.answers.slice()
    kept.push(line)
    this.answers = kept
    this.answerOpen = true
  }

  private appendAnswer(delta: string): void {
    if (!this.answerOpen || delta === '' || this.answers.length === 0) return
    const kept = this.answers.slice()
    const last = kept[kept.length - 1]
    kept[kept.length - 1] = { ...last, text: last.text + delta }
    this.answers = kept
  }

  private beginTurn(id: string): boolean {
    const ordinal = this.turnOrdinal(id)
    if (ordinal === null) return false
    if (ordinal <= this.highestTurn) return false
    this.updateLastAnswer({ status: 'interrupted' })
    this.silenceLocal()
    this.highestTurn = ordinal
    if (ordinal > this.observedControlTurn) this.observedControlTurn = ordinal
    this.activeTurnId = id
    this.playbackAcks.clear()
    this.playbackSyncFailed = false
    return true
  }

  // ackServer=false is voor een beurt waarin nooit geluid heeft geklonken. De
  // afspeelstand lokaal bijwerken mag altijd; hem bij de server melden niet.
  // De server heeft na een mislukte beurt zijn generatie al opgehoogd, dus een
  // nagekomen bevestiging wordt geweigerd en zet playbackSyncFailed — "het
  // geluid speelde niet volledig af" over een beurt die geen geluid had.
  //
  // holdPause=false is voor een beurt die de beurtklok stopte (turn_timeout):
  // dat is geen afspeelfout, en de server pauzeert er ook niet op. Een lokale
  // pauze zou de duif hier stil houden terwijl de server haar weer laat
  // meedoen.
  private onPlayback(state: PlaybackState, ackServer = true, holdPause = true): void {
    if (this.handlingPlaybackFailure) return
    if (state === 'failed') {
      // A known gap/decoder failure is terminal locally. An ACK or server
      // echo may never arrive; retained PCM must not outlive the silent UI.
      this.handlingPlaybackFailure = true
      try { if (this.speaks) this.silenceLocal() } finally { this.handlingPlaybackFailure = false }
      if (holdPause) this.holdLocalPause()
      this.updateLastAnswer({ status: 'interrupted' })
      this.manualTurnPending = false
      this.manualTurnActive = false
    }
    this.playbackStatus = state
    if (state === 'started') this.speaking = true
    else if (this.generationComplete || !this.speaks || state === 'aborted' || state === 'failed') this.speaking = false
    if (!ackServer || !this.playbackTracking || !this.activeTurnId || !this.sessionId) return
    const turnId = this.activeTurnId
    const key = `${turnId}:${state}`
    if (this.playbackAcks.has(key)) return
    this.playbackAcks.add(key)
    const generation = this.sessionGen
    void this.send(ROUTES.playback, { turnId, state }).then(async (res) => {
      if (generation !== this.sessionGen || turnId !== this.activeTurnId) return
      if (!res || !res.ok) { this.playbackSyncFailed = true; return }
      const body = await res.json().catch(() => null) as { accepted?: boolean; settings?: SettingsDTO } | null
      if (generation !== this.sessionGen || turnId !== this.activeTurnId) return
      if (body?.settings) this.observeControlSettings(body.settings)
      if (body?.accepted === false) this.playbackSyncFailed = true
    })
  }

  // Wat de gebruiker niet kan verhelpen — weggevallen audioblokken, een
  // bereikte kostenlimiet, een onbruikbaar detectieresultaat — staat niet op
  // het scherm en gaat ook niet naar de console: modulecode logt niets
  // (frontend-wachter). De tellers blijven in de store; de server logt zelf.

  private updateLastAnswer(patch: Partial<AnswerLine>): void {
    if (!this.answerOpen || this.answers.length === 0) return
    this.answers = this.answers.map((line, index) => index === this.answers.length - 1 ? { ...line, ...patch } : line)
  }

  private finishSignal(id?: string): void {
    if (id && this.signal?.id !== id) return
    this.cancelSignal?.()
    this.cancelSignal = null
    this.signal = null
  }

  private onSound(ev: MeetingEvent): void {
    if (!ev.sound?.url || (this.autonomyPaused && !this.manualTurnPending)) return
    const id = ev.eventId || `legacy-${++this.legacySignalSequence}`
    if (this.seenSignals.has(id)) return
    if (this.seenSignals.size >= 4096) { this.signalError = true; return }
    if (ev.turnId && !this.beginTurn(ev.turnId)) return
    this.seenSignals.add(id)
    this.manualTurnPending = false
    this.sounds.silence()
    this.finishSignal()
    // A selected field recording replaces the synthetic event sample locally.
    // Real playback callbacks still own sound status; no audio is synthesized here.
    // Het native pakket heeft één opname per band. De band reist
    // met de gebeurtenis mee en kiest hier het bestand; hij wordt nergens
    // getoond, benoemd of uitgelegd. Een gebeurtenis zonder bekende
    // band houdt het geluid dat de server zelf koos.
    const band = ev.sound.band
    const selectedSound = (SIGNAL_BANDS as readonly string[]).includes(band ?? '')
      ? mediaCatalogue.signalSounds?.[band as SignalBand] ?? null : null
    const eventDuration = selectedSound?.durationMs ?? ev.durationMs
    const durationMs = typeof eventDuration === 'number' && Number.isFinite(eventDuration)
      ? Math.max(200, Math.min(15000, eventDuration)) : 1500
    this.signal = { id, durationMs }
    this.signalError = false
    this.stopped = false
    this.generationComplete = true
    this.playbackStatus = 'pending'
    this.signalPlaybackId = id
    // A bounded visible fallback also works without the final recording or
    // video. It never assigns a meaning to the signal.
    this.cancelSignal = this.ports.schedule(() => {
      if (this.audioBlocked || this.signalError || this.signalPlaybackId !== id) this.finishSignal(id)
    }, durationMs)
    if (this.audioBlocked) { this.signalError = true; this.onPlayback('failed'); return }
    this.cancelSignalWatchdog?.()
    this.cancelSignalWatchdog = this.ports.schedule(() => {
      if (this.signalPlaybackId !== id) return
      this.sounds.silence()
      this.signalError = true
      this.onPlayback('failed')
    }, 15000)
    try {
      const handle = this.ports.playSound(selectedSound?.url ?? ev.sound.url)
      this.sounds.play({
        stop: () => {
          handle.stop()
          if (this.signalPlaybackId === id) { this.cancelSignalWatchdog?.(); this.signalPlaybackId = ''; this.onPlayback('aborted'); this.finishSignal(id) }
        },
        onEnded: (done) => handle.onEnded(() => {
          done()
          if (this.signalPlaybackId !== id) return
          this.cancelSignalWatchdog?.()
          this.signalPlaybackId = ''
          if (!this.signalError) { this.finishSignal(id); this.onPlayback('ended') }
        }),
      })
      handle.onStarted?.(() => { if (this.signalPlaybackId === id) this.onPlayback('started') })
      handle.onError?.(() => {
        if (this.signalPlaybackId !== id) return
        this.signalError = true
        this.onPlayback('failed')
        this.cancelSignal?.()
        this.cancelSignal = this.ports.schedule(() => this.finishSignal(id), 1500)
        // Keep the bounded visual fallback; a failed medium is still visible.
      })
    } catch { this.signalError = true; this.onPlayback('failed') }
  }

  async checkDevices(): Promise<void> {
    if (this.phase !== 'setup' || this.deviceCheck === 'checking') return
    this.cancelDeviceTest()
    this.bindPageHandlers()
    const generation = ++this.deviceCheckGeneration
    this.deviceCheck = 'checking'
    this.deviceCheckHeard = false
    this.deviceCheckAudio = false
    let ctx: AudioContextLike
    try { ctx = this.ports.createAudioContext() } catch { this.deviceCheck = 'unavailable'; this.unbindPageHandlers(); return }
    this.checkContext = ctx
    try { await ctx.resume() } catch { /* status below remains truthful */ }
    if (generation !== this.deviceCheckGeneration) return
    this.deviceCheckAudio = ctx.state === 'running'
    try {
      const mic = await this.ports.openMic(ctx, (block) => {
        if (generation === this.deviceCheckGeneration && block.some((sample) => Math.abs(sample) > 600)) this.deviceCheckHeard = true
      })
      if (generation !== this.deviceCheckGeneration) { mic.stop(); return }
      this.checkMic = mic
      this.cancelDeviceCheck = this.ports.schedule(() => {
        this.cancelDeviceTest()
        this.deviceCheck = 'ready'
      }, DEVICE_CHECK_MS)
    } catch {
      if (generation !== this.deviceCheckGeneration) return
      this.cancelDeviceTest()
      this.deviceCheck = 'refused'
    }
  }

  cancelDeviceTest(): void {
    this.deviceCheckGeneration++
    this.cancelDeviceCheck?.()
    this.cancelDeviceCheck = null
    this.checkMic?.stop()
    this.checkMic = null
    const ctx = this.checkContext
    this.checkContext = null
    if (ctx) void ctx.close().catch(() => {})
    if (this.deviceCheck === 'checking') this.deviceCheck = 'idle'
    if (this.phase === 'setup') this.unbindPageHandlers()
  }

  async retryMicrophone(): Promise<void> {
    if (!this.live || this.localMic === 'opening' || this.localMic === 'open' || this.localMic === 'muted') return
    this.micNotice = ''
    this.micFailure = null
    this.serverListen = LISTEN.reconnecting
    this.sluice.close()
    this.sluice = this.createSluice()
    if (!this.ctx) { this.ctx = this.createContext(); void this.unblockAudio(this.sessionGen) }
    await this.openMic(this.sessionGen)
  }

  async retryDetector(): Promise<void> {
    if (!this.live || !this.detector?.retryable || this.detectorRetryInFlight) return
    const generation = this.sessionGen
    this.detectorRetryInFlight = true
    const res = await this.send(ROUTES.detectorRetry)
    if (generation !== this.sessionGen) return
    this.detectorRetryInFlight = false
    if (!res) { this.failWith('common.connection.lost', { status: 0 }); return }
    if (!res.ok) { await this.failFrom(res, 'error.generic', generation); return }
    const body = await res.json().catch(() => null) as { detector?: MeetingEvent['detector'] } | null
    if (generation === this.sessionGen && body?.detector) this.detector = { ...body.detector }
  }

  async resumeAutonomy(): Promise<void> {
    if (!this.live || !this.autonomyPaused || this.resumeInFlight) return
    const generation = this.sessionGen
    const intent = this.pauseRevision
    const observedTurn = this.observedControlTurn.toString()
    this.resumeInFlight = true
    try {
      const res = await this.send(ROUTES.resume, { turnId: observedTurn })
      if (generation !== this.sessionGen || intent !== this.pauseRevision) return
      if (!res) { this.failWith('common.connection.lost', { status: 0 }); return }
      if (!res.ok) {
        const body = await res.clone().json().catch(() => null) as { settings?: SettingsDTO } | null
        if (generation !== this.sessionGen || intent !== this.pauseRevision) return
        if (body?.settings) this.observeControlSettings(body.settings)
        await this.failFrom(res, 'error.generic', generation)
        return
      }
      const body = await res.json().catch(() => null) as { autonomyPaused?: boolean; settings?: SettingsDTO } | null
      if (generation !== this.sessionGen || intent !== this.pauseRevision) return
      if (body?.autonomyPaused !== false || body.settings?.turnId !== observedTurn) { this.playbackSyncFailed = true; return }
      this.confirmResume(body.settings?.turnId)
    } finally {
      if (generation === this.sessionGen && intent === this.pauseRevision) this.resumeInFlight = false
    }
  }

  private holdLocalPause(newIntent = true): void {
    this.localPauseHeld = true
    if (newIntent) { this.pauseRevision++; this.resumeInFlight = false; this.settingsMayResume = false }
    this.autonomyPaused = true
  }

  private applyAutonomyPause(value: unknown): void {
    if (typeof value === 'boolean') this.autonomyPaused = this.localPauseHeld || value
  }

  private confirmResume(turnId?: string): void {
    const through = turnId ? this.turnOrdinal(turnId) : null
    if (through !== null) {
      if (through < this.lastStopBarrier) return
      this.resolvedStopThrough = this.resolvedStopThrough > through ? this.resolvedStopThrough : through
    }
    this.localPauseHeld = false
    this.autonomyPaused = false
    this.stopPending = false
    this.stopped = false
    this.playbackSyncFailed = false
    this.stopSyncFailed = false
  }

  private turnOrdinal(id: string): bigint | null {
    return id.length <= 20 && /^\d+$/.test(id) ? BigInt(id) : null
  }

  private acceptStopBarrier(id: string): boolean {
    const ordinal = this.turnOrdinal(id)
    if (ordinal === null || ordinal < this.highestTurn || ordinal <= this.resolvedStopThrough) return false
    this.highestTurn = ordinal
    if (ordinal > this.observedControlTurn) this.observedControlTurn = ordinal
    this.lastStopBarrier = ordinal
    return true
  }

  // --- knoppen --------------------------------------------------------------

  // triggerTurn is de gedeelde vorm van de twee beurtknoppen: de pratende duif
  // antwoordt, de non-verbale grijpt in. Alleen de route, het al dan
  // niet loslaten van het thema en het legen van de microfoonsluis verschillen.
  private async triggerTurn(route: string, clearTrigger: boolean, flushMic = false): Promise<void> {
    if (this.respondInFlight || !this.sessionId) return
    if (clearTrigger) this.trigger = null
    this.busy = null
    this.error = null
    // De foutmelding verdween en liet de geluidsmelding van de mislukte beurt
    // eronder zichtbaar worden. Die hoort bij de beurt die nu vervangen wordt.
    this.playbackSyncFailed = false
    this.respondInFlight = true
    this.manualTurnPending = true
    this.stopped = false
    const gen = this.sessionGen
    // Eerst het laatste geluid de lijn op, dan pas de vraag. De
    // server rondt de lopende uiting zelf af, maar hij kan alleen afronden wat
    // hij al binnen heeft; wat hier nog in de sluis staat is precies de zin
    // waarop geantwoord moet worden.
    const draining = flushMic ? this.sluice.flush(MIC_FLUSH_MS) : null
    if (draining) {
      await draining
      if (gen !== this.sessionGen) return // de vergadering is intussen een andere
      // En in dat venster (hooguit MIC_FLUSH_MS) kan de stopknop gevallen zijn.
      // Die is de gegarandeerde stilte: wie stopt hoort de duif dáárna niet
      // alsnog beginnen omdat haar vraag nog in de wacht stond.
      if (this.stopped) {
        this.respondInFlight = false
        this.manualTurnPending = false
        return
      }
    }
    const res = await this.send(route)
    if (gen !== this.sessionGen) return // een laat 409 van een vorige sessie
    this.respondInFlight = false
    if (res === null) {
      this.manualTurnPending = false
      this.failWith('common.connection.lost', { status: 0 })
      return
    }
    if (!res.ok) { this.manualTurnPending = false; await this.failFrom(res, 'error.generic', gen) }
  }

  // Deze beurt komt van de knop, niet van een thema: het thema mag weg. En zij
  // leest het venster, dus het laatste geluid moet eerst de server bereiken.
  respond(): Promise<void> {
    return this.triggerTurn(ROUTES.respond, true, true)
  }

  // De interventie van de non-verbale duif is een geluid uit haar eigen
  // set: zij leest geen venster, dus daar valt niets op te wachten.
  intervene(): Promise<void> {
    return this.triggerTurn(ROUTES.intervene, false)
  }

  // speakCandidate is de knop "laat horen" onder het kandidaatblok: de duif
  // spreekt haar klaarliggende bijdrage meteen uit, langs de stilte-eis. De
  // knop verdwijnt zodra de server de kandidaat afgeeft (de server wist hem).
  async speakCandidate(): Promise<void> {
    if (this.candidateInFlight || !this.sessionId || !this.candidate) return
    this.candidateInFlight = true
    this.busy = null
    this.error = null
    this.playbackSyncFailed = false
    const gen = this.sessionGen
    const res = await this.send(ROUTES.candidateSpeak)
    if (gen !== this.sessionGen) return
    this.candidateInFlight = false
    if (res === null) {
      this.failWith('common.connection.lost', { status: 0 })
      return
    }
    // Een 409 betekent dat de kandidaat er niet meer is: het blok is dan al
    // weg via het kanaal, en er valt niets te melden.
    if (!res.ok && res.status !== 409) await this.failFrom(res, 'error.generic', gen)
  }

  // stop is de stopknop: eerst lokaal stil, dan pas de server. Of de
  // melding aankomt verandert niets aan de stilte hier.
  async stop(): Promise<void> {
    this.stopSyncFailed = false
    this.holdLocalPause()
    this.silenceLocal()
    this.autonomyPaused = true
    this.manualTurnPending = false
    this.manualTurnActive = false
    this.updateLastAnswer({ status: 'stopped' })
    this.answerOpen = false
    this.stopped = true
    this.stopPending = true
    this.speaking = false
    this.busy = null
    if (!this.sessionId) {
      this.stopPending = false
      return
    }
    const gen = this.sessionGen
    const res = await this.send(ROUTES.stop)
    if (gen !== this.sessionGen) return
    // De 202 zegt alleen dat het verzoek is aangenomen. Pas het `stopped`- (of
    // `persona`-, `state`-, `session_closed`-)bericht sluit de beurt echt af;
    // tot dan blijft een hartslag die "spreekt" roept genegeerd.
    // Hier is het stil, maar de server weet van niets: dat is een storing in de
    // bediening en krijgt haar eigen melding, niet die over afgekapt geluid.
    // Die melding vraagt om nog een keer stoppen, dus zij hoort alleen bij een
    // storing waar dat bij helpen kan: geen verbinding of een serverfout (5xx;
    // de algemene foutmelding zou haar anders verdringen). Een 404 is
    // een sessie die al dicht is; session_closed ruimt dan op. Een verlopen
    // login of een geweigerd verzoek gaat de gewone foutroute: daar helpt
    // opnieuw drukken niet.
    if (res === null || res.status >= 500) { this.stopPending = false; this.stopSyncFailed = true }
    else if (!res.ok) {
      this.stopPending = false
      if (res.status !== 404) await this.failFrom(res, 'error.generic', gen)
    }
    if (res?.ok) {
      const body = await res.json().catch(() => null) as { settings?: SettingsDTO } | null
      if (gen === this.sessionGen && body?.settings) this.observeControlSettings(body.settings)
    }
  }

  // acknowledgePresence is elk antwoord op "Ben je er nog?" behalve
  // beëindigen: de knop, Escape en een klik naast de dialoog. Wie de vraag
  // wegdrukt, is er duidelijk nog. De dialoog gaat meteen dicht.
  // Haalt het antwoord de server niet, dan komt de vraag terug met de tijd die
  // er nog was: de server stopt de vergadering anders toch, en een dichte
  // dialoog zou dan liegen. Een 404 is geen storing — de sessie was al dicht,
  // en de sluitmelding is onderweg.
  async acknowledgePresence(): Promise<void> {
    const question = this.presence
    this.presence = null
    this.cancelPresenceRedraw?.()
    this.cancelPresenceRedraw = null
    if (!this.sessionId) return
    const gen = this.sessionGen
    this.holdInteraction()
    this.presenceAnswering = true
    const res = await this.send(ROUTES.presence)
    if (gen !== this.sessionGen) return
    this.presenceAnswering = false
    // Niet aangekomen is ook: aangekomen en geweigerd (5xx, 401). Alleen een
    // 404 zegt dat de sessie al weg is; dan sluit de vraag en volgt de melding.
    const lost = res === null || (!res.ok && res.status !== 404)
    if (lost && question && this.presenceDeadline > 0 && !this.presence) {
      this.presence = question
      this.redrawPresence()
      return
    }
    if (!this.presence) this.closePresence()
  }

  // followPresence leest de resterende tijd op de hartslag. Zonder tijd staat
  // er op de server geen vraag (meer) open: iemand antwoordde in een ander
  // tabblad of klikte. Met tijd staat zij er wel: dan schuift de aftelling
  // hooguit naar voren (de server is de klok, een late hartslag zet haar niet
  // terug), en is zij hier dicht terwijl er geen "ja" onderweg is, dan komt
  // zij terug.
  private followPresence(graceMs: number | undefined): void {
    const open = typeof graceMs === 'number' && graceMs > 0
    if (!open) {
      if (this.presence) this.closePresence()
      return
    }
    if (!this.presence) {
      if (!this.presenceAnswering) this.openPresence(this.presenceMessage, graceMs)
      return
    }
    const deadline = this.ports.now() + graceMs
    if (this.presenceDeadline === 0 || deadline < this.presenceDeadline) this.setPresenceDeadline(deadline)
    this.redrawPresence()
  }

  // openPresence zet de vraag op het scherm en de aftelling op de klok van de
  // browser: deadline = nu + de resterende tijd die de server meegaf.
  private openPresence(message: Bilingual | undefined, graceMs: number | undefined): void {
    this.presenceMessage = message ?? {}
    this.presence = this.presenceMessage
    this.presenceAnswering = false
    // Deze vraag gaat voor elke andere (AC12). De vragen om te beëindigen of
    // van duif te wisselen delen dezelfde laag en lagen er anders bovenop, met
    // de focus op een knop die niemand zag. Wie terug is, kan ze opnieuw
    // stellen.
    this.endConfirm = false
    this.pendingSwitch = ''
    this.cancelPresenceFallback?.()
    this.cancelPresenceFallback = null
    if (typeof graceMs !== 'number' || !(graceMs > 0)) {
      this.presenceDeadline = 0
      return
    }
    this.setPresenceDeadline(this.ports.now() + graceMs)
    this.redrawPresence()
  }

  // setPresenceDeadline zet de deadline en plant de lokale terugval opnieuw,
  // zodat die meeschuift als een hartslag de aftelling naar voren haalt.
  private setPresenceDeadline(deadline: number): void {
    this.presenceDeadline = deadline
    this.cancelPresenceFallback?.()
    const gen = this.sessionGen
    this.cancelPresenceFallback = this.ports.schedule(() => {
      this.cancelPresenceFallback = null
      if (gen === this.sessionGen) this.presenceExpired()
    }, Math.max(0, deadline - this.ports.now()) + PRESENCE_FALLBACK_MS)
  }

  // redrawPresence leest de klok en plant de volgende tel, zolang de vraag er
  // staat.
  private redrawPresence(): void {
    this.presenceNow = this.ports.now()
    this.cancelPresenceRedraw?.()
    this.cancelPresenceRedraw = this.presence && this.presenceDeadline > 0
      ? this.ports.schedule(() => { this.cancelPresenceRedraw = null; this.redrawPresence() }, PRESENCE_REDRAW_MS)
      : null
  }

  private closePresence(): void {
    this.presence = null
    this.presenceAnswering = false
    this.presenceDeadline = 0
    this.cancelPresenceRedraw?.()
    this.cancelPresenceRedraw = null
    this.cancelPresenceFallback?.()
    this.cancelPresenceFallback = null
  }

  // presenceExpired is de lokale terugval: de aftelling is ruim voorbij en er
  // kwam geen session_closed. Dan is het netwerk weg, en dan legt de browser de
  // vergadering zelf neer — met de reden die de server ook gegeven had.
  private presenceExpired(): void {
    if (this.presenceDeadline === 0 || !this.sessionId) return
    if (this.ports.now() < this.presenceDeadline + PRESENCE_FALLBACK_MS) return
    this.layDown(CLOSE_REASONS.inactivity, true)
  }

  // updateSettings zet de knoppen meteen goed en stuurt de wijziging uitgesteld
  // door: één POST per bedoeling in plaats van één per schuifstapje.
  updateSettings(patch: Partial<MeetingSettingsState>): void {
    this.settingsRevision++
    this.settingsChosen = true
    // Zelfstandig meedoen heeft een eigen vlag, want het heeft als enige een
    // standaard per duif. Zonder die scheiding bevroor het verzetten van
    // gretigheid of cooldown ook de stand van de schakelaar, en stond de
    // signalerende duif op uit terwijl de hint eronder "standaard aan" zei.
    if (patch.autonomous !== undefined) this.autonomyChosen = true
    if (patch.autonomous === false) this.settingsMayResume = false
    if (patch.autonomous === true && !this.settings.autonomous) this.settingsMayResume = true
    const next = { ...this.settings, ...patch }
    next.cooldownSeconds = clampCooldown(next.cooldownSeconds)
    this.settings = next
    if (!this.sessionId) return
    const gen = this.sessionGen
    this.cancelSettings?.()
    this.cancelSettings = this.ports.schedule(() => {
      this.cancelSettings = null
      void this.pushSettings(gen)
    }, SETTINGS_DEBOUNCE_MS)
  }

  private settingsPending(): boolean {
    return this.cancelSettings !== null || this.settingsInFlight > 0
  }

  private async pushSettings(gen: number): Promise<void> {
    const revision = this.settingsRevision
    const intent = this.pauseRevision
    const wantsResume = this.settingsMayResume
    const body = {
      autonomous: this.settings.autonomous,
      eagerness: this.settings.eagerness,
      cooldownSeconds: this.settings.cooldownSeconds,
    }
    this.settingsInFlight++
    try {
      const res = await this.send(ROUTES.settings, body)
      if (gen !== this.sessionGen) return // een late fout van een vorige sessie
      if (!res) this.failWith('common.connection.lost', { status: 0 })
      else if (!res.ok) await this.failFrom(res, 'error.generic', gen)
      else if (wantsResume && body.autonomous && this.settings.autonomous) {
        const confirmed = await res.json().catch(() => null) as SettingsDTO | null
        if (gen !== this.sessionGen || intent !== this.pauseRevision || revision !== this.settingsRevision) return
        this.settingsMayResume = false
        if (confirmed) this.observeControlSettings(confirmed)
        if (confirmed?.autonomyPaused === false) this.confirmResume(confirmed.turnId)
        else if (this.autonomyPaused) await this.resumeAutonomy()
      }
    } finally {
      this.settingsInFlight--
    }
  }

  private observeControlSettings(settings: SettingsDTO): void {
    const ordinal = settings.turnId ? this.turnOrdinal(settings.turnId) : null
    if (ordinal === null) return
    if (ordinal > this.observedControlTurn) this.observedControlTurn = ordinal
    if (settings.autonomyPaused && ordinal > this.lastStopBarrier) this.lastStopBarrier = ordinal
  }

  private applySettings(settings: SettingsDTO | undefined): void {
    if (!settings) return
    this.observeControlSettings(settings)
    this.settings = {
      autonomous: typeof settings.autonomous === 'boolean' ? settings.autonomous : this.settings.autonomous,
      eagerness: isEagerness(settings.eagerness) ? settings.eagerness : this.settings.eagerness,
      cooldownSeconds:
        typeof settings.cooldownSeconds === 'number' ? clampCooldown(settings.cooldownSeconds) : this.settings.cooldownSeconds,
    }
  }

  // setMuted dempt het spoor én leegt de sluis: wat daar stond haalt de duif
  // niet meer, dus het telt als gemist geluid in plaats van als vertraagde
  // waarheid.
  setMuted(next: boolean): void {
    this.muted = next
    this.mic?.setEnabled(!next)
    if (next) {
      this.partial = null
      this.sluice.drop()
      if (this.localMic === 'open') this.localMic = 'muted'
    } else if (this.mic && this.localMic === 'muted') {
      this.localMic = 'open'
    }
  }

  end(): void {
    this.endConfirm = false
    this.layDown('', true)
  }

  // askEnd is wat de grote rode knop doet: beëindigen, maar niet midden in een
  // zin zonder te vragen. De vraag komt alleen als er nog gesproken wordt; de
  // knop zelf blijft dus in alle andere gevallen één klik.
  askEnd(): void {
    if (this.speaking) {
      this.endConfirm = true
      return
    }
    this.end()
  }

  cancelEnd(): void {
    this.endConfirm = false
  }

  // --- vastzitten door een eigen open vergadering ----------------------

  // De melding krijgt een uitweg, maar niet zonder de gebruiker: eerst de
  // vraag, dan pas het beëindigen van die andere vergadering.
  askReplace(): void {
    if (this.stuck) this.stuckConfirm = true
  }

  cancelReplace(): void {
    this.stuckConfirm = false
  }

  async confirmReplace(): Promise<void> {
    if (!this.stuckConfirm || this.replaceInFlight) return
    this.replaceInFlight = true
    this.stuckConfirm = false
    this.error = null
    try {
      await this.start(true)
    } finally {
      this.replaceInFlight = false
    }
  }

  // --- een andere duif aan tafel --------------------------------------

  // De duifkiezer bovenaan doet tijdens een vergadering iets: hij stelt de
  // vraag. Zonder lopende vergadering, of bij de duif die er al zit, is er
  // niets te vragen.
  requestPigeonSwitch(pigeonId: string): boolean {
    if (!this.live || this.phase !== 'live') return false
    if (!pigeonId || pigeonId === this.pigeonId || this.switchInFlight) return false
    if (!this.meetingPigeons.some((p) => p.id === pigeonId)) return false
    this.pendingSwitch = pigeonId
    return true
  }

  cancelPigeonSwitch(): void {
    this.pendingSwitch = ''
  }

  // confirmPigeonSwitch wisselt van duif zonder de vergadering te verlaten. Het
  // is een nieuwe sessie die de context van de oude overneemt (de server doet
  // die overdracht in één open-verzoek), geen sessie die van duif verandert:
  // persona, stem, thema's en detector liggen vast zodra een duif aan tafel
  // zit, en gescheiden gesprekken per duif is een kerninvariant. Wat blijft
  // staan is alles wat van de vergadering is: de microfoon, de audiocontext,
  // de instellingen en het transcript op het scherm.
  async confirmPigeonSwitch(): Promise<void> {
    const next = this.pendingSwitch
    this.pendingSwitch = ''
    if (!next || !this.live || this.switchInFlight) return
    const previous = this.sessionId
    if (!previous) return

    this.switchInFlight = true
    // Eerst stil: wat de vorige duif nog zei hoort niet over de nieuwe heen te
    // klinken. Daarna gaat het kanaal dicht en is elke late voortzetting van
    // de oude sessie ongeldig — de microfoon blijft open (eigen generatie).
    this.silenceLocal()
    this.channel?.close()
    this.channel = null
    const gen = ++this.sessionGen
    this.sluice.close()

    let res: Response | null = null
    try {
      // `replaceExisting` staat er met opzet naast `handoverFrom`: een wissel
      // is altijd een vervanging van de eigen plaats. Is de sessie waarnaar we
      // wijzen intussen vanzelf gesloten, dan verliest de wissel zijn context,
      // maar hij hoort nooit te stranden op "er is al een vergadering open".
      res = await this.ports.fetch(
        API + ROUTES.sessions,
        jsonInit(this.openBody({ pigeonId: next, handoverFrom: previous, replaceExisting: true }, next)),
      )
    } catch {
      res = null
    }
    if (gen !== this.sessionGen) {
      // Onderweg ingehaald (einde, unmount): wat er toch geopend werd sluiten.
      this.switchInFlight = false
      if (res?.ok) await this.discard(res)
      return
    }
    const data = res?.ok ? ((await res.json().catch(() => null)) as OpenResponse | null) : null
    if (!data?.sessionId) {
      // De wissel ging niet door. De oude vergadering leeft nog op de server —
      // die vervangt hij pas bij een geslaagd openen — dus we gaan er gewoon
      // weer op zitten, met een melding erbij.
      if (res) await this.failFrom(res, 'meeting.switch.failed', gen)
      else this.failWith('meeting.switch.failed', { status: 0 })
      this.sessionId = previous
      this.sluice = this.createSluice()
      this.openChannel(gen)
      this.switchInFlight = false
      return
    }

    // De vorige duif laat haar bijdragen niet achter op het scherm van de
    // nieuwe: haar woorden blijven van haar, en de server draagt ze als
    // "wat de vorige duif zei" over aan de nieuwe duif. Haar laatste bijdrage
    // blijft wél op de kaart staan — onder haar eigen naam, en met de melding
    // dat de wissel haar afbrak als zij nog aan het woord was — zodat de tafel
    // niet leeg lijkt tot de nieuwe duif iets zegt.
    this.carried = this.carriedFrom(this.answers.at(-1))
    this.answers = []
    this.sources = []
    this.kbStatus = undefined
    this.activeTurnId = ''
    this.playbackStatus = 'idle'
    this.playbackAcks.clear()
    this.seenSignals.clear()
    this.busy = null
    this.stopped = false
    this.stopPending = false
    this.respondInFlight = false
    this.manualTurnPending = false
    this.manualTurnActive = false
    this.highestTurn = 0n
    this.observedControlTurn = 0n
    this.arousal = 0
    // De kandidaat van de vorige duif is van haar: bij een wissel verdwijnt
    // het blok "de duif heeft iets over ..." met haar mee.
    this.candidate = null
    this.candidateInFlight = false
    this.trigger = null
    this.detector = null
    this.detectorRetryInFlight = false
    // Een Stop gold voor de vorige duif; de nieuwe begint gewoon.
    this.localPauseHeld = false
    this.resolvedStopThrough = -1n
    this.lastStopBarrier = -1n
    this.pauseRevision++

    this.sessionId = data.sessionId
    this.pigeonId = data.pigeonId || next
    this.lang = isLang(data.lang) ? data.lang : this.lang
    this.voice = isVoiceState(data.voice) ? data.voice : VOICE.off
    this.serverListen = isListenState(data.listening) ? data.listening : LISTEN.off
    this.applySettings(data.settings ?? { autonomous: data.autonomous, eagerness: data.eagerness })
    this.autonomyPaused = data.autonomyPaused === true
    this.playbackTracking = data.playbackTracking === true
    this.sluice = this.createSluice()
    this.openChannel(gen)
    this.switchInFlight = false
  }

  // carriedFrom maakt van de laatste kaart van de vorige duif de kaart die
  // tijdens de wissel blijft staan. Een kaart zonder tekst laat niets zien en
  // wordt niets; stond zij nog te schrijven, dan zegt de kaart dat de wissel
  // haar afbrak. De bronnen blijven eraan hangen: het is dezelfde bijdrage,
  // alleen van de duif die net wegging.
  private carriedFrom(line: AnswerLine | undefined): AnswerLine | null {
    if (!line?.text.trim()) return null
    return line.status === 'streaming' ? { ...line, status: 'interrupted' } : line
  }

  // --- microfoon -------------------------------------------------------

  private async openMic(gen: number): Promise<void> {
    // Het spoor hangt aan zijn eigen generatie, niet aan de sessie: een
    // duifwissel vervangt de sessie en laat de microfoon open staan.
    // Alleen closeMic en het neerleggen van de vergadering sluiten hem.
    const mine = ++this.micGeneration
    const open = () => mine === this.micGeneration
    // Het openen zelf hoort wél bij deze sessie: wordt er tijdens het wachten
    // op toestemming een andere vergadering begonnen, dan is dit spoor weg.
    const current = () => open() && gen === this.sessionGen
    const ctx = this.ctx
    if (!ctx) {
      this.localMic = 'refused'
      this.micNotice = 'meeting.mic.none'
      return
    }
    this.localMic = 'opening'
    let mic: MicLike
    try {
      mic = await this.ports.openMic(ctx, (block) => { if (open()) this.onBlock(block) })
    } catch {
      if (!current()) return
      this.localMic = 'refused'
      this.micNotice = 'meeting.mic.refused'
      return
    }
    if (!current()) {
      mic.stop()
      return
    }
    this.mic = mic
    this.micStream = mic.stream
    this.localMic = this.muted ? 'muted' : 'open'
    if (this.muted) mic.setEnabled(false)
  }

  // onBlock is de tweede poort van het dempen. De port sluit de bron al af,
  // maar een implementatie die dat niet doet (of een blok dat de wissel net
  // gemist heeft) komt hier niet langs: gedempt is gedempt.
  private onBlock(block: Int16Array): void {
    if (this.muted) return
    this.sluice.push(block)
  }

  private closeMic(): void {
    this.audioGeneration++
    this.micGeneration++
    this.serverListen = LISTEN.off
    this.mic?.stop()
    this.mic = null
    this.micStream = null
    // De sluis gaat óók dicht: na een weigering blijft het scherm niet
    // doorpompen: de knop blijft, het geluid niet.
    this.sluice.close()
    this.localMic = 'closed'
  }

  private createSluice(): AudioSluice {
    const session = this.sessionGen
    const transport = ++this.audioGeneration
    const current = () => session === this.sessionGen && transport === this.audioGeneration
    return new AudioSluice({
      fetch: this.ports.fetch,
      url: () => current() && this.sessionId ? this.sessionUrl(this.sessionId) + ROUTES.audio : null,
      onDropped: (count: number) => { if (current()) this.droppedLocal += count },
      onResponse: (res: Response) => current() ? this.onAudioResponse(res, session, transport) : undefined,
      schedule: this.ports.schedule,
    })
  }

  private async onAudioResponse(res: Response, gen: number, transport: number): Promise<void> {
    if (res.ok) {
      const body = (await res.json().catch(() => null)) as { dropped?: number; listening?: unknown } | null
      if (gen !== this.sessionGen || transport !== this.audioGeneration || !body) return
      // Wat de server niet kwijt kon telt net zo hard mee als wat wij lieten
      // vallen: de vergadering ziet één getal.
      if (typeof body.dropped === 'number' && body.dropped > 0) this.droppedServer += body.dropped
      // De stand op een audioantwoord geldt alleen zolang het kanaal openstaat:
      // het geluid haalde de server wel, maar het antwoord van de duif komt via
      // het kanaal, en dat herstelt zich nog.
      if (isListenState(body.listening) && !this.channelDown) this.serverListen = body.listening
      return
    }
    if (res.status === 404) {
      // De sessie bestaat niet meer: microfoon dicht en wachten op het
      // session_closed-bericht dat de rest opruimt.
      this.closeMic()
      return
    }
    // Eerst stil, dán lezen: het lezen van de foutbody kan hangen op een trage
    // lijn, en zolang dat duurt zou de microfoon vrolijk blijven doorpompen.
    this.closeMic()
    const closedTransport = this.audioGeneration
    const failure = await readFailure(res)
    if (gen !== this.sessionGen || closedTransport !== this.audioGeneration) return
    // Geen STT is geen algemene storing: het is precies de melding dat de duif
    // niet meeluistert, en die hoort bij de microfoon.
    this.micFailure = failure
  }

  // --- stilte, opruimen -----------------------------------------------------

  // silenceLocal is de lokale helft van de stopknop: alles wat geluid kan maken
  // hangt hieronder — de speelrij, de lopende uiting én de veldopnames.
  private silenceLocal(): void {
    this.mediaStopRevision++
    this.player.silence()
    this.sounds.silence()
    this.finishSignal()
    this.collector.reset()
    this.pcm.reset()
    this.turnFormat = null
    this.acceptTts = false
  }

  // dispose legt de vergadering neer. Idempotent: een tweede aanroep doet niets
  // meer (één DELETE, één ctx.close(), geen throw).
  dispose(reason = ''): void {
    this.configurationGeneration++
    this.cancelDeviceTest()
    this.layDown(reason, true)
  }

  private layDown(reason: string, notifyServer: boolean): void {
    // Viel de verbinding weg terwijl de aftelling al op nul stond, dan was dat
    // niet de reden: de vergadering stopte omdat niemand antwoordde.
    if (reason === CLOSE_CONNECTION_LOST && this.presenceDeadline > 0 && this.ports.now() >= this.presenceDeadline) {
      reason = CLOSE_REASONS.inactivity
    }
    this.mediaStopRevision++
    const sessionId = this.sessionId
    this.cancelDeviceTest()
    // Elke lopende async-voortzetting is hiermee ongeldig.
    this.sessionGen++
    this.unbindPageHandlers()
    this.cancelSettings?.()
    this.cancelSettings = null

    // 1. de sporen: de microfoon gaat als eerste dicht.
    this.micGeneration++
    this.mic?.stop()
    this.mic = null
    this.micStream = null
    this.sluice.close()

    // 2. het geluid, inclusief wat er nog stond te decoderen.
    this.player.silence()
    this.sounds.silence()
    this.finishSignal()
    this.collector.reset()
    this.pcm.reset()
    this.turnFormat = null
    const ctx = this.ctx
    this.ctx = null
    if (ctx) void ctx.close().catch(() => {})

    // 3. het kanaal.
    this.channel?.close()
    this.channel = null

    // 4. het scherm: er is geen kopie, dus loslaten is wissen.
    this.clearScreen()
    this.serverListen = LISTEN.off
    this.localMic = 'closed'
    this.muted = false
    this.voice = VOICE.off

    // 5. de server, zonder erop te wachten: hier is het al stil.
    this.sessionId = ''
    if (notifyServer && sessionId) this.deleteSession(sessionId)

    this.phase = 'setup'
    this.live = false
    this.disposed = true
    this.closedReason = reason
    if (reason === '') this.error = null
  }

  private resetSession(): void {
    this.clearScreen()
    this.error = null
    this.closedReason = ''
    this.muted = false
    this.transcriptSeq = 0
    this.answerSeq = 0
  }

  // clearScreen is "er is geen kopie": alles wat één sessie lang op het scherm
  // stond, en elke melding die erbij hoorde. Gedeeld door het opruimen achteraf
  // en het schoonvegen vooraf, zodat er geen tweede lijst kan gaan afwijken.
  private clearScreen(): void {
    this.localPauseHeld = false
    this.resolvedStopThrough = -1n
    this.lastStopBarrier = -1n
    this.pauseRevision++
    this.finishSignal()
    this.cancelSignalWatchdog?.()
    this.cancelSignalWatchdog = null
    this.signalPlaybackId = ''
    this.highestTurn = 0n
    this.observedControlTurn = 0n
    this.activeTurnId = ''
    this.playbackAcks.clear()
    this.playbackStatus = 'idle'
    this.playbackTracking = false
    this.playbackSyncFailed = false
    this.stopSyncFailed = false
    this.seenSignals.clear()
    this.manualTurnPending = false
    this.manualTurnActive = false
    this.autonomyPaused = false
    this.resumeInFlight = false
    this.signalError = false
    this.transcript = []
    this.answers = []
    this.carried = null
    this.partial = null
    this.closePresence()
    this.cancelInteractionHold?.()
    this.cancelInteractionHold = null
    this.interactionPending = false
    // De vraag of je echt wilt beëindigen gaat mee met het scherm: sluit de
    // server de sessie ondertussen zelf, dan staat er geen dialoog meer over een
    // vergadering die er niet meer is. De vraag om van duif te wisselen ook,
    // anders staat hij bij de volgende vergadering meteen open.
    this.endConfirm = false
    this.pendingSwitch = ''
    this.candidate = null
    this.candidateInFlight = false
    this.trigger = null
    this.detector = null
    this.detectorRetryInFlight = false
    this.settingsMayResume = false
    this.acceptTts = false
    this.answerOpen = false
    this.droppedLocal = 0
    this.droppedServer = 0
    this.arousal = 0
    this.speaking = false
    this.stopped = false
    this.stopPending = false
    this.respondInFlight = false
    this.audioGap = false
    this.voiceUnavailable = false
    this.audioBlocked = false
    this.sources = []
    this.kbStatus = undefined
    this.busy = null
    this.micNotice = ''
    this.micFailure = null
  }

  // Beëindigen is altijd keepalive. Een gewone fetch wordt afgebroken zodra de
  // pagina wegnavigeert — precies het moment waarop dit bericht het hardst
  // nodig is — en dan blijft de sessie op de server staan. Er zit geen
  // body op, dus de keepalive-limiet is niet in zicht.
  private deleteSession(sessionId: string): void {
    void this.ports.fetch(this.sessionUrl(sessionId), { method: 'DELETE', keepalive: true }).catch(() => {})
  }

  // --- pagina-levenscyclus --------------------------------------------------

  private readonly onPageHide = (): void => {
    const sessionId = this.sessionId
    // Local resources close before any network request, including a pending
    // permission prompt or opening request. bfcache never revives a microphone.
    this.layDown(CLOSE_CONNECTION_LOST, false)
    if (sessionId) this.deleteSession(sessionId)
  }

  private readonly onPageShow = (ev: PageTransitionEvent): void => {
    // Terug uit de bfcache: kanaal en microfoon zijn weg, de sessie op de
    // server ook. Eerlijk als beëindigd tonen in plaats van een dood scherm.
    if (ev.persisted && this.sessionId) this.layDown(CLOSE_CONNECTION_LOST, false)
  }

  // Een klik, aanraking of toets ergens in de pagina — op elk tabblad, ook in
  // de uitleg — is het teken van leven voor de inactiviteitsbewaking. In de
  // vangfase, zodat geen onderdeel hem kan wegvangen. Muisbewegen en scrollen
  // tellen niet: dat doet een tafel die tegen de laptop stoot ook. Staat de
  // vraag zelf open, dan antwoordt alleen de dialoog: een Tab-toets daarin is
  // nog geen "ja". Een klik in een ánder browsertabblad telt niet: dat heeft
  // geen sessie, en een berichtenkanaal tussen tabbladen is in deze module
  // bewust verboden (meeting_frontend_test.go).
  private readonly onInteraction = (): void => {
    if (!this.sessionId || this.phase !== 'live' || this.presence) return
    if (this.cancelInteractionHold) {
      this.interactionPending = true
      return
    }
    this.reportInteraction()
  }

  private reportInteraction(): void {
    this.interactionPending = false
    void this.send(ROUTES.presence)
    this.holdInteraction()
  }

  // holdInteraction start de drossel. Gebeurde er tijdens de drossel iets, dan
  // gaat er aan het eind precies één signaal achteraan — tenzij de vraag
  // intussen openstaat: een klik van vóór de vraag is geen antwoord erop.
  private holdInteraction(): void {
    this.cancelInteractionHold?.()
    this.cancelInteractionHold = this.ports.schedule(() => {
      this.cancelInteractionHold = null
      if (this.presence) this.interactionPending = false
      else if (this.interactionPending && this.sessionId) this.reportInteraction()
    }, INTERACTION_THROTTLE_MS)
  }

  // Terug naar een tabblad dat in de achtergrond stond: de aftelling klopt
  // meteen weer, en is zij ruim voorbij, dan stopt de vergadering hier ook.
  private readonly onVisibility = (): void => {
    if (!this.presence) return
    this.redrawPresence()
    this.presenceExpired()
  }

  private bindPageHandlers(): void {
    if (this.bound || typeof globalThis.addEventListener !== 'function') return
    this.bound = true
    globalThis.addEventListener('pagehide', this.onPageHide)
    globalThis.addEventListener('pageshow', this.onPageShow)
    globalThis.addEventListener('pointerdown', this.onInteraction, true)
    globalThis.addEventListener('keydown', this.onInteraction, true)
    globalThis.addEventListener('visibilitychange', this.onVisibility)
  }

  private unbindPageHandlers(): void {
    if (!this.bound) return
    this.bound = false
    globalThis.removeEventListener('pagehide', this.onPageHide)
    globalThis.removeEventListener('pageshow', this.onPageShow)
    globalThis.removeEventListener('pointerdown', this.onInteraction, true)
    globalThis.removeEventListener('keydown', this.onInteraction, true)
    globalThis.removeEventListener('visibilitychange', this.onVisibility)
  }

  // --- kleine hulpjes -------------------------------------------------------

  private sessionUrl(sessionId: string): string {
    return API + ROUTES.sessions + '/' + sessionId
  }

  // send doet één besturingsbericht en gooit nooit: een wegvallende verbinding
  // is een melding, geen kapot scherm. null = niet aangekomen.
  private async send(path: string, body?: unknown): Promise<Response | null> {
    if (!this.sessionId) return null
    const url = this.sessionUrl(this.sessionId) + path
    try {
      return await this.ports.fetch(url, body === undefined ? { method: 'POST' } : jsonInit(body))
    } catch {
      return null
    }
  }

  // failWith is de synchrone helft: een fout die we zelf al kennen. Ruwe
  // fouttekst bereikt de gebruiker nooit — alleen de tweetalige servermelding
  // of onze eigen tekst.
  private failWith(fallback: MsgKey, failure: ApiFailure): void {
    this.errorFallback = fallback
    this.error = failure
  }

  // failFrom leest de foutbody van een antwoord. Dat lezen kost een await, en
  // in die tijd kan de sessie zijn ingehaald; dan hoort de melding bij een
  // vergadering die niet meer bestaat en blijft het scherm schoon.
  private async failFrom(res: Response, fallback: MsgKey, gen: number): Promise<void> {
    const failure = await readFailure(res)
    if (gen !== this.sessionGen) return
    this.failWith(fallback, failure)
  }

  private renderClosed(reason: string): string {
    if (!reason) return ''
    const known: readonly string[] = [...Object.values(CLOSE_REASONS), CLOSE_CONNECTION_LOST]
    // Een onbekende reden is een woord van de server; dat hoort niet ongefilterd
    // op het scherm. De sessie is dicht, alleen het waarom ontbreekt.
    const key = 'meeting.closed.' + reason
    const detail = known.includes(reason) && isMsgKey(key) ? this.locale.t(key) : this.locale.t('meeting.closed.unknown')
    return this.locale.t('meeting.closed') + ' (' + detail + ').'
  }
}
