// De WebVTT van de uitlegvideo, voor de speler in de help en in de
// etalage: alleen wat de duif laat horen, haar uitgesproken zinnen en
// "[pigeon sound]", elk met haar naam. De naamsvermelding van het model staat
// ingebrand in beeld en in de voet van de site; als ondertitel lag zij er in de
// etalage (ondertitels standaard aan) met twee URL's bovenop. De tijdlijn
// bewaart haar wel.
export function captionsVtt(cues) {
  for (const c of cues) {
    if (!c.speaker) throw new Error(`ondertitel zonder spreker: ${JSON.stringify(c.text)}; alleen de duif hoort in de ondertitels`)
  }
  const ts = (s) => {
    const ms = Math.round(s * 1000)
    const h = Math.floor(ms / 3600000)
    const m = Math.floor((ms % 3600000) / 60000)
    const sec = ((ms % 60000) / 1000).toFixed(3).padStart(6, '0')
    return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${sec}`
  }
  const sorted = [...cues].sort((a, b) => a.start - b.start)
  return 'WEBVTT\n\n' + sorted.map((c, k) => `${k + 1}\n${ts(c.start)} --> ${ts(c.end)}\n<v ${c.speaker}>${c.text}\n`).join('\n')
}
