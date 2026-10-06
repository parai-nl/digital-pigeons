import type { Fetcher } from '../api'
import { BLOCK_MS, BLOCK_SAMPLES, Decimator, floatToInt16 } from './pcm'

// De microfoonkant van de vergadering, zonder browser-API's: hier zit wat
// er met de monsters gebeurt nadat de port ze heeft opgehaald, en hoe ze de
// lijn op gaan. De capture zelf (getUserMedia, ScriptProcessor) staat in
// lib/audio/browser.ts.

// BlockAssembler is de trechter tussen de audiocallback en de sluis: hij
// bemonstert naar 16 kHz en knipt de stroom in vaste blokken van 4096 monsters
// (256 ms). Wat er aan het eind van een blok overblijft wacht op het volgende;
// er wordt niets verzameld dat niet meteen doorgaat.
export class BlockAssembler {
  private readonly decimator: Decimator
  private buffer = new Int16Array(BLOCK_SAMPLES)
  private filled = 0

  constructor(
    inputRate: number,
    private readonly emit: (block: Int16Array) => void,
  ) {
    this.decimator = new Decimator(inputRate)
  }

  push(samples: Float32Array): void {
    const down = this.decimator.process(samples)
    const pcm = floatToInt16(down)
    let offset = 0
    while (offset < pcm.length) {
      const room = BLOCK_SAMPLES - this.filled
      const take = Math.min(room, pcm.length - offset)
      this.buffer.set(pcm.subarray(offset, offset + take), this.filled)
      this.filled += take
      offset += take
      if (this.filled === BLOCK_SAMPLES) {
        const block = this.buffer
        // Een vers blok voor het volgende stuk: het verstuurde blok mag niet
        // onder de handen van de sluis veranderen.
        this.buffer = new Int16Array(BLOCK_SAMPLES)
        this.filled = 0
        this.emit(block)
      }
    }
  }

  // reset laat de halve blok los; wat er stond gaat nergens heen.
  reset(): void {
    this.buffer = new Int16Array(BLOCK_SAMPLES)
    this.filled = 0
    this.decimator.reset()
  }
}

// De sluis is doorstroomruimte, geen opslag. Twaalf blokken zijn ruim drie
// seconden: genoeg om een trage lijn te overbruggen, kort genoeg om verlies
// eerlijk te melden in plaats van een oplopende achterstand te verbergen.
export const MAX_SLUICE_BLOCKS = 12

export interface SluiceHooks {
  fetch: Fetcher
  // url geeft de audioroute van de huidige sessie, of null als er geen sessie
  // (meer) is; zo raakt een nagekomen blok nooit een volgende vergadering.
  url(): string | null
  onDropped(count: number): void
  onResponse(res: Response): void | Promise<void>
  // schedule plant de rustpauze na een mislukte POST en levert de annulering,
  // zodat close() geen timer laat naklinken. Verplicht: de sluis kent geen
  // browsertimer, dat is precies wat haar in jsdom toetsbaar houdt.
  schedule(fn: () => void, ms: number): () => void
}

// AudioSluice houdt één verzoek tegelijk in de lucht. Audio die door elkaar
// aankomt is voor de STT erger dan audio die wegvalt, dus de volgorde wint;
// wat er tijdens een lopende POST binnenkomt wacht en gaat mee in het volgende.
export class AudioSluice {
  private queue: Int16Array[] = []
  private inFlight = false
  private closed = false
  // drained zijn de wachters van flush(): de knop die wil dat het laatste
  // geluid de server bereikt vóórdat hij om een beurt vraagt.
  private drained: (() => void)[] = []
  // resting: na een mislukte POST ligt de sluis één blokinterval stil. Zonder
  // die pauze zou elk volgend blok de kapotte lijn meteen opnieuw proberen en
  // de sluis in een lus van mislukkingen trekken.
  private resting = false
  private cancelRest: (() => void) | null = null

  constructor(private readonly hooks: SluiceHooks) {}

  push(block: Int16Array): void {
    if (this.closed) return
    if (this.queue.length >= MAX_SLUICE_BLOCKS) {
      this.hooks.onDropped(1)
      return
    }
    this.queue.push(block)
    void this.pump()
  }

  // flush stuurt wat er nog in de sluis staat meteen weg en wacht tot de lijn
  // leeg is — hooguit maxWaitMs lang. De spreekknop gebruikt hem: de sluis mag
  // tot drie seconden audio onderweg hebben, en juist de laatste zin vóór de
  // knopdruk is meestal de vraag aan de duif. Hij wacht nooit
  // onbegrensd: een haperende lijn mag de knop niet ophouden, en wat er dan
  // nog staat gaat mee met het volgende blok.
  //
  // null betekent: er valt niets te legen. Dat scheelt de aanroeper niet
  // alleen een await maar ook een microtaak — en in die ene microtaak kan de
  // vergadering hem inhalen (een sessiewissel, een dispose), waarna zijn
  // verzoek nergens meer heen gaat. Een sluis in rust ná een mislukte
  // verzending telt daar ook toe: die lijn gaf net een fout, en de knop laten
  // wachten op een herkansing die de beurt toch niet meer haalt kost alleen
  // tijd.
  flush(maxWaitMs: number): Promise<void> | null {
    if (this.closed || this.resting || this.atRest()) return null
    return new Promise((resolve) => {
      let cancel: (() => void) | null = null
      const finish = (): void => {
        cancel?.()
        cancel = null
        this.drained = this.drained.filter((w) => w !== finish)
        resolve()
      }
      this.drained.push(finish)
      cancel = this.hooks.schedule(finish, maxWaitMs)
      void this.pump()
    })
  }

  // atRest: er staat niets meer te wachten en er is niets meer onderweg.
  private atRest(): boolean {
    return !this.inFlight && this.queue.length === 0
  }

  // settleDrained laat de wachters van flush() gaan zodra de sluis leeg is —
  // of zodra zij dicht is, want dan komt er niets meer door.
  private settleDrained(): void {
    if (this.drained.length === 0 || (!this.closed && !this.atRest())) return
    const waiting = this.drained
    this.drained = []
    for (const done of waiting) done()
  }

  // drop leegt de sluis en telt wat erin stond als gemist geluid: dat is wat
  // dempen doet. Afsluiten telt niet mee — dan is er niets meer om te missen —
  // en gaat via close().
  drop(): void {
    if (this.queue.length > 0) this.hooks.onDropped(this.queue.length)
    this.queue = []
    this.settleDrained()
  }

  close(): void {
    this.closed = true
    this.queue = []
    this.stopResting()
    this.settleDrained()
  }

  private stopResting(): void {
    const cancel = this.cancelRest
    this.cancelRest = null
    this.resting = false
    cancel?.()
  }

  private rest(): void {
    this.resting = true
    this.cancelRest = this.hooks.schedule(() => {
      this.cancelRest = null
      this.resting = false
      void this.pump()
    }, BLOCK_MS)
  }

  private async pump(): Promise<void> {
    if (this.inFlight || this.closed || this.resting || this.queue.length === 0) {
      this.settleDrained()
      return
    }
    const url = this.hooks.url()
    if (url === null) {
      this.queue = []
      this.settleDrained()
      return
    }
    const blocks = this.queue
    this.queue = []
    let length = 0
    for (const b of blocks) length += b.length
    const pcm = new Int16Array(length)
    let at = 0
    for (const b of blocks) {
      pcm.set(b, at)
      at += b.length
    }
    this.inFlight = true
    let res: Response
    try {
      res = await this.hooks.fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/octet-stream' },
        body: pcm.buffer,
      })
    } catch {
      // Een mislukte verzending is verloren geluid: geteld, en daarna één
      // blokinterval rust zodat een haperende lijn niet in een lus loopt.
      this.inFlight = false
      this.hooks.onDropped(blocks.length)
      if (!this.closed) this.rest()
      // Wat weg is komt niet meer: wie op deze verzending wachtte mag door.
      this.settleDrained()
      return
    }
    this.inFlight = false
    if (this.closed) {
      this.settleDrained()
      return
    }
    if (!res.ok) {
      // Geen STT, gesloten sessie: de store meldt het en sluit de microfoon.
      this.queue = []
      this.settleDrained()
      await this.hooks.onResponse(res)
      return
    }
    await this.hooks.onResponse(res)
    void this.pump()
  }
}
