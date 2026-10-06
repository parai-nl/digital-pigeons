// De handelingen per scène, gedeeld door opnemen en afspelen. Bij
// het opnemen gaat alles zo snel als de app toelaat; bij het afspelen bepaalt
// `ctl.pace` hoe lang een beeld blijft staan. De volgorde van de klikken is in
// beide standen dezelfde, en daarmee ook de volgorde van de verzoeken — daar
// rust het koppelen van antwoorden aan verzoeken op (lib/match.mjs).

// De vraag die de chattake stelde. Een nieuwe opname gebruikt deze; afspelen
// typt altijd de vraag uit de take zelf (take.input.question).
export const QUESTION = 'Pigeons are rats with wings. Would culling them actually solve anything for Rotterdam?'

// De scènes die opgenomen en afgespeeld kunnen worden.
export const SCENES = {
  chat: { kind: 'chat' },
  'meeting-nonverbal': { kind: 'meeting', pigeon: 'nonverbal', autonomous: true },
  'meeting-individualist': { kind: 'meeting', pigeon: 'individualist', autonomous: true },
  // Tweede richting: de duif doet niet zelf mee; na het
  // fragment drukt de begeleider op de knop, en de duif antwoordt.
  'meeting-nonverbal-respond': { kind: 'meeting', pigeon: 'nonverbal', autonomous: false, respond: true },
  'meeting-individualist-respond': { kind: 'meeting', pigeon: 'individualist', autonomous: false, respond: true },
}

// De bron die de chat opent en de keten in de kennisgraaf erachter. De
// citatie is het bronnummer uit het antwoord; de graaf gaat van die bevinding
// naar haar letterlijke citaat en naar het artikel.
const SOURCE_NUMBER = '6'

// ctl: { mode, base, question, mark(name, data), pace(key), waitFor(pred, ms, label, from), since(), checkAnswer(page, n) }
export async function chatScene(page, ctl) {
  ctl.mark('load')
  await page.goto(ctl.base + '/duif', { waitUntil: 'domcontentloaded' })
  await page.getByTestId('chat-empty').waitFor({ timeout: 20000 })
  await page.locator('.pigeon-choice[data-pigeon="biological"]').waitFor()
  await waitForPortraits(page)
  await startPointer(page, ctl)
  await ctl.pace('settle')
  ctl.mark('start-screen')
  await ctl.pace('start')

  ctl.mark('type')
  const input = page.getByTestId('chat-input')
  await click(page, ctl, input)
  if (ctl.mode === 'record') await input.fill(ctl.question)
  else await input.pressSequentially(ctl.question, { delay: ctl.typingDelay })
  ctl.mark('typed')
  await ctl.pace('beforeSend')
  const firstTurn = ctl.since()
  ctl.mark('send')
  await click(page, ctl, page.getByTestId('chat-send-button'))
  await ctl.waitFor((r) => r.kind === 'http-end' && r.path?.endsWith('/chat/turn'), 180000, 'eerste beurt', firstTurn)
  await page.getByTestId('ask-other-pigeon').and(page.locator(':enabled')).waitFor()
  ctl.mark('answer-done')
  await ctl.checkAnswer?.(page, 0)
  await ctl.pace('readFirst')

  // Een bronnummer in het antwoord opent de bronlade: de samenvatting van de
  // bron en de oorspronkelijke woorden.
  ctl.mark('source-open')
  await click(page, ctl, page.locator('.duif-cite', { hasText: new RegExp(`^${SOURCE_NUMBER}$`) }).first())
  await page.getByTestId('source-drawer').waitFor()
  await page.getByTestId('source-quote').first().waitFor()
  ctl.mark('source-shown')
  await ctl.pace('source')

  // Vanuit de lade de kennisgraaf in: de bevinding in haar omgeving, dan het
  // volgende niveau (de citaten en het artikel eromheen), dan het artikel zelf.
  ctl.mark('graph-open')
  await click(page, ctl, page.getByTestId('source-graph-button'))
  const detail = page.getByTestId('graph-detail')
  await detail.waitFor({ timeout: 20000 })
  await page.getByTestId('detail-expand').waitFor()
  ctl.mark('graph-shown')
  // Bij het openen vliegt de kaart naar de bevinding, maar de indeling schuift
  // daarna nog even door, en waar de bevinding dan ligt hangt af van hoe snel
  // alles binnenkwam: soms buiten beeld (gezien na de graaf zonder
  // perspectieven). Zodra de kaart stilligt, kiest de bezoeker de bevinding opnieuw
  // in de boom, met Enter op de rij die al gekozen is; de camera vliegt er dan
  // heen. Geen verzoek, alleen bij het afspelen.
  if (ctl.mode === 'replay') {
    await ctl.pace('graphSettle')
    const row = page.locator('[data-testid="tree-item"]:has(> .graph-tree-row.selected)').first()
    await row.focus()
    // Kan de rij geen focus krijgen, dan stopt de capture: een stille terugval
    // zou de bevinding misschien buiten beeld laten.
    if (!(await row.evaluate((el) => el === document.activeElement))) throw new Error('kennisgraaf: de gekozen bevinding in de boom krijgt geen focus, dus de camera kan er niet heen')
    await page.keyboard.press('Enter')
    ctl.mark('graph-refocus')
  }
  await ctl.pace('graph')
  ctl.mark('graph-expand')
  await click(page, ctl, page.getByTestId('detail-expand'))
  const paper = detail.locator('.detail-group').last().locator('button').first()
  await detail.locator('.detail-group').nth(2).waitFor({ timeout: 20000 })
  await ctl.pace('graphExpand')
  ctl.mark('graph-paper')
  await click(page, ctl, paper)
  await page.locator('[data-testid="graph-detail"][data-type="interview"]').waitFor()
  // Het leespaneel houdt de scrollstand van het vorige item; de bezoeker
  // scrolt naar boven, naar de naam en de verwijzing van het artikel.
  await detail.evaluate((el) => el.scrollTo({ top: 0 }))
  await ctl.pace('graphPaper')

  // Terug naar het gesprek en dezelfde vraag aan de andere duif.
  ctl.mark('graph-back')
  await click(page, ctl, page.getByRole('button', { name: /Back to/ }))
  await page.getByTestId('ask-other-pigeon').waitFor()
  await ctl.pace('back')
  const secondTurn = ctl.since()
  ctl.mark('ask-other')
  await moveTo(page, ctl, page.getByTestId('ask-other-pigeon').last())
  await ctl.pace('hoverAskOther')
  ctl.mark('ask-other-click')
  await click(page, ctl, page.getByTestId('ask-other-pigeon').last())
  await ctl.waitFor((r) => r.kind === 'http-chunk' && r.types?.includes('artifact_delta'), 180000, 'antwoord Stakeholder', secondTurn)
  ctl.mark('answer2-shown')
  // Haar antwoord in beeld: de lijn "The same question, now to Stakeholder"
  // bovenaan, dan de facilitator, haar naam en haar antwoord.
  await page.waitForTimeout(150)
  await scrollThread(page, '[data-testid="repeated-question"]')
  ctl.mark('answer2-framed')
  await ctl.waitFor((r) => r.kind === 'http-end' && r.path?.endsWith('/chat/turn'), 180000, 'tweede beurt', secondTurn)
  ctl.mark('answer2-done')
  await ctl.checkAnswer?.(page, 1)
  await ctl.pace('readSecond')
  ctl.mark('end')
}

// meetingScene zet één duif aan tafel terwijl het vergaderfragment door de
// synthetische microfoon gaat, laat het hele fragment uitspelen en beëindigt
// daarna netjes. Bij het opnemen reageert de duif zoals zij wil (zelfstandig);
// zwijgt een pratende duif na het fragment, dan drukt de driver op "Let the
// pigeon respond" — echte bediening. Bij het afspelen gebeuren dezelfde
// klikken op de opgenomen momenten, door de tijdkaart gehaald.
export async function meetingScene(page, ctl, { pigeon, autonomous, respond = false, timing }) {
  ctl.mark('load')
  await page.goto(ctl.base + '/duif', { waitUntil: 'domcontentloaded' })
  await page.locator('.pigeon-choice[data-pigeon="biological"]').waitFor({ timeout: 20000 })
  await startPointer(page, ctl)
  await click(page, ctl, page.locator('#duif-tab-meeting'))
  await page.getByTestId('seat-button').waitFor()
  await click(page, ctl, page.locator(`.pigeon-choice[data-pigeon="${pigeon}"]`))
  await page.locator(`[data-testid="meeting-view"][data-pigeon="${pigeon}"]`).waitFor()
  await waitForPortraits(page)
  await ctl.pace('setup')
  const toggle = page.getByTestId('autonomy-field').getByRole('switch')
  if (!(await toggle.isVisible())) await click(page, ctl, page.getByTestId('meeting-view').locator('summary').first())
  if ((await toggle.getAttribute('aria-checked')) !== String(autonomous)) {
    ctl.mark('autonomy-toggle')
    await click(page, ctl, toggle)
    await ctl.pace('toggle')
  }
  if ((await toggle.getAttribute('aria-checked')) !== String(autonomous)) throw new Error('zelfstandig meedoen staat niet zoals gevraagd')

  const opened = ctl.since()
  ctl.mark('seat')
  await click(page, ctl, page.getByTestId('seat-button'))
  await park(page, ctl)
  await page.getByTestId('live-view').waitFor({ timeout: 20000 })
  const channel = await ctl.waitFor((r) => r.kind === 'es-construct', 20000, 'vergaderkanaal', opened)
  ctl.mark('channel', { at: channel.t })
  // Het hele verloop in beeld: "Full transcript" open. Alleen weergave, geen
  // verzoek. De opname deed het; bij het afspelen kan het dicht blijven
  // (ctl.fullTranscript, video.config.json): dan toont de app haar
  // standaardstand, de laatste twee zinnen onder de duif.
  if (respond && ctl.fullTranscript !== false) {
    await click(page, ctl, page.getByTestId('transcript-toggle'))
    await page.getByTestId('transcript-log').waitFor()
    await park(page, ctl)
    ctl.mark('transcript-open')
  } else if (respond && ctl.mode === 'replay') {
    await holdPigeonInView(page)
  }
  const button = page.getByTestId(pigeon === 'nonverbal' ? 'intervene-button' : 'respond-button')
  // Een reactie telt pas als zij af is: een geluid, of een bijdrage die tot
  // response_done kwam. Een bijdrage die de server zelf afbrak omdat er nog
  // gesproken werd (stopped, context_changed) is geen reactie.
  const reacted = (r) => r.kind === 'es-message' && (r.type === 'sound' || r.type === 'response_done')

  if (ctl.mode === 'record' && respond) {
    // Na het laatste woord (en de laatste regel in het verloop) de knop.
    const mic = await ctl.waitFor((r) => r.kind === 'mic-open', 20000, 'microfoon', opened)
    await ctl.sleepUntil(mic.t + timing.dialogueEndMs + timing.respondGapMs)
    ctl.mark('respond-click')
    await click(page, ctl, button)
    await ctl.waitFor(reacted, 90000, 'reactie na de knop', opened)
    await settle(ctl, opened)
  } else if (ctl.mode === 'record') {
    const mic = await ctl.waitFor((r) => r.kind === 'mic-open', 20000, 'microfoon', opened)
    // Het hele fragment plus stilte: de duif mag op elk moment reageren.
    await ctl.sleepUntil(mic.t + timing.dialogueEndMs + timing.quietMs)
    if (!ctl.find(reacted, opened)) {
      ctl.mark('respond-click')
      await click(page, ctl, button)
      await ctl.waitFor(reacted, 60000, 'reactie na de knop', opened)
    }
    await settle(ctl, opened)
  } else {
    if (timing.pressAtMs != null) {
      // De knop gaat in op het moment waarop hij dat in de take deed
      // (pressAtMs, net vóór haar reactie). Later mag niet: dan reageert de
      // duif al vóór de klik in beeld, en negeert de app de klik omdat zij al
      // bezig is. De muis vertrekt daarom eerder en doet er POINT_MS over, zodat
      // een kijker ziet waar hij heen gaat.
      const pressAt = channel.t + timing.pressAtMs
      await ctl.sleepUntil(pressAt - POINT_MS - AIM_MS)
      await moveTo(page, ctl, button, POINT_MS)
      await ctl.sleepUntil(pressAt)
      ctl.mark('respond-click')
      const clicked = ctl.since()
      await press(page, ctl, button)
      // De app moet de klik ook echt doen: zonder haar verzoek zou de reactie
      // in beeld van de planning komen en niet van de knop.
      await ctl.waitFor((r) => r.kind === 'http-request' && /\/(respond|intervene)$/.test(r.path ?? ''), 3000, 'het verzoek van de knop', clicked)
      // Het verloop staat onderaan het podium; haar bijdrage verschijnt
      // bovenaan, naast de duif. Zoals een kijker doet: naar boven scrollen
      // zodra zij iets zegt. De signalerende duif zegt niets: houdt het podium
      // haar al helemaal in beeld (holdPigeonInView), dan blijft het staan en
      // blijven de laatste zinnen leesbaar.
      const held = ctl.fullTranscript === false && pigeon === 'nonverbal'
      if (respond) await park(page, ctl)
      if (respond && !held) {
        await releasePigeonHold(page)
        await ctl.waitFor((r) => r.kind === 'es-message' && ['persona', 'sound'].includes(r.type), 30000, 'reactie', clicked)
        await page.locator('.stage-scroll').evaluate((el) => el.scrollTo({ top: 0, behavior: 'smooth' }))
        ctl.mark('stage-top')
      }
    }
    await ctl.sleepUntil(channel.t + timing.endAtMs)
    await ctl.checkMeeting?.(page)
  }

  ctl.mark('end-click')
  await page.getByTestId('call-control-end').click()
  const dialog = page.getByTestId('end-dialog-confirm')
  await Promise.race([dialog.waitFor({ timeout: 5000 }).then(() => dialog.click()), page.getByTestId('seat-button').waitFor({ timeout: 5000 })]).catch(() => {})
  await page.getByTestId('seat-button').waitFor({ timeout: 10000 })
  ctl.mark('ended')
  await ctl.pace('afterEnd')
}

// holdPigeonInView doet wat een kijker doet als het podium hoger is dan het
// scherm: zo ver omlaag scrollen dat de laatste zinnen onder de duif te lezen
// zijn, maar nooit zo ver dat de bovenkant van de duif wegvalt. De v4-duiven
// vullen hun beeld tot de bovenrand; een vergadering die tot
// onderaan scrolt, snijdt hun kop af. Het podium groeit als er een zin
// bijkomt, dus de stand wordt bij elke maatverandering opnieuw gezet; bij een
// pratende duif tot de knop (releasePigeonHold). Alleen bij het afspelen; de
// app merkt er niets van.
async function holdPigeonInView(page) {
  await page.locator('.stage-scroll .pigeon-canvas').waitFor()
  await page.evaluate(() => {
    const scroller = document.querySelector('.stage-scroll')
    const canvas = scroller.querySelector('.pigeon-canvas')
    const hold = () => {
      if (!window.__uvHoldPigeon) return
      const top = scroller.scrollTop + canvas.getBoundingClientRect().top - scroller.getBoundingClientRect().top
      scroller.scrollTop = Math.min(Math.floor(top), scroller.scrollHeight - scroller.clientHeight)
    }
    window.__uvHoldPigeon = true
    const observer = new ResizeObserver(hold)
    for (const child of scroller.children) observer.observe(child)
    hold()
  })
}

async function releasePigeonHold(page) {
  await page.evaluate(() => { window.__uvHoldPigeon = false })
}

// Zo lang glijdt de muis naar de knop van de vergadering, en zo lang richt hij
// daarna nog; samen ruim een halve seconde vóór het indrukken.
const POINT_MS = 450
const AIM_MS = 120

// settle wacht tot wat de duif begon ook af is: een geluid tot zijn einde,
// een bijdrage tot haar laatste woord en haar laatste klank.
async function settle(ctl, from) {
  const deadline = Date.now() + 180000
  while (Date.now() < deadline) {
    // Een geluid telt vanaf het `sound`-bericht, niet pas vanaf het afspelen:
    // tussen die twee kan het laden liggen.
    const sounds = Math.max(ctl.all((r) => r.kind === 'media-play', from).length, ctl.all((r) => r.kind === 'es-message' && r.type === 'sound', from).length)
    const ended = ctl.all((r) => r.kind === 'media-ended', from).length
    const personas = ctl.all((r) => r.kind === 'es-message' && r.type === 'persona', from).length
    const done = ctl.all((r) => r.kind === 'es-message' && ['response_done', 'error', 'stopped'].includes(r.type), from).length
    if (sounds <= ended && personas <= done && Date.now() > ctl.pcmEnd() + 800) break
    await new Promise((r) => setTimeout(r, 250))
  }
  ctl.mark('reaction-done')
  await new Promise((r) => setTimeout(r, 2500))
}

// scrollThread scrolt het gesprek zo dat het laatste element dat bij de
// selector hoort bovenaan het gesprek staat, met een zachte beweging.
async function scrollThread(page, selector) {
  await page.evaluate((sel) => {
    const target = [...document.querySelectorAll(sel)].at(-1)
    const scroller = document.querySelector('[data-testid="message-list-scroll"]')
    if (!target || !scroller) return
    const top = scroller.scrollTop + target.getBoundingClientRect().top - scroller.getBoundingClientRect().top - 12
    scroller.scrollTo({ top: Math.max(0, top), behavior: 'smooth' })
  }, selector)
  await page.waitForTimeout(500)
}

// --- de muis ---------------------------------------------------------------
// Bij afspelen staat er een muis in beeld (session.mjs tekent hem): hij glijdt
// naar wat er aangeklikt wordt, zodat een kijker ziet waar er geklikt wordt.
// Bij opnemen gaat een klik gewoon direct.
export async function moveTo(page, ctl, locator, ms) {
  await locator.scrollIntoViewIfNeeded()
  if (ctl.mode !== 'replay') return locator.hover()
  const box = await locator.boundingBox()
  if (!box) return locator.hover()
  const to = { x: box.x + box.width / 2, y: box.y + box.height / 2 }
  await glide(page, ctl, to, ms)
}

export async function click(page, ctl, locator) {
  await moveTo(page, ctl, locator)
  if (ctl.mode !== 'replay') return locator.click()
  await page.waitForTimeout(ctl.aimMs ?? 180)
  await press(page, ctl, locator)
}

// press klikt waar de muis staat, zonder dat Playwright nog op het element
// wacht: de knop van de vergadering wordt na de klik meteen "bezig", en een
// wachtende klik hield de driver twintig seconden vast.
async function press(page, ctl, locator) {
  // Ligt het doel na het scrollen niet onder de muis (een paneel dat nog
  // verschoof), dan eerst opnieuw richten; lukt dat niet, dan een gewone klik.
  let hit = await under(locator, ctl.pointer)
  if (!hit) { await moveTo(page, ctl, locator); hit = await under(locator, ctl.pointer) }
  if (!hit) return locator.click()
  await page.mouse.down()
  await page.mouse.up()
}

function under(locator, point) {
  return locator.evaluate((el, { x, y }) => { const top = document.elementFromPoint(x, y); return Boolean(top && (el === top || el.contains(top))) }, point).catch(() => false)
}

// park zet de muis opzij als er een tijd niets te klikken valt.
export async function park(page, ctl) {
  if (ctl.mode !== 'replay') return
  const v = page.viewportSize()
  await glide(page, ctl, { x: v.width * 0.95, y: v.height * 0.78 })
}

// De muis begint rustig rechts in beeld, waar hij niets bedekt.
async function startPointer(page, ctl) {
  if (ctl.mode !== 'replay') return
  const v = page.viewportSize()
  ctl.pointer = { x: v.width * 0.9, y: v.height * 0.72 }
  await page.mouse.move(ctl.pointer.x, ctl.pointer.y)
}

async function glide(page, ctl, to, fixedMs) {
  const from = ctl.pointer ?? to
  const distance = Math.hypot(to.x - from.x, to.y - from.y)
  const ms = fixedMs ?? Math.min(700, Math.max(220, distance * 0.9))
  const steps = Math.max(2, Math.round(ms / 16))
  // Elke stap op zijn eigen moment vanaf het begin, zodat de beweging echt
  // `ms` duurt en niet ook de tijd van elke muisopdracht erbij optelt.
  const start = Date.now()
  for (let i = 1; i <= steps; i++) {
    const u = i / steps
    const e = u < 0.5 ? 4 * u * u * u : 1 - Math.pow(-2 * u + 2, 3) / 2
    await page.mouse.move(from.x + (to.x - from.x) * e, from.y + (to.y - from.y) * e)
    await page.waitForTimeout(Math.max(0, start + (ms * i) / steps - Date.now()))
  }
  ctl.pointer = to
}

// De portretten laden lui; een scène begint pas als ze er staan.
async function waitForPortraits(page) {
  await page.waitForFunction(() => [...document.images].filter((img) => img.src.includes('/media/')).every((img) => img.complete && img.naturalWidth > 0), null, { timeout: 15000 }).catch(() => {})
}
