// Server-Sent Events van de module lezen, met het moment waarop elk frame
// binnenkwam. Een take bewaart de ruwe brokken zoals ze over de lijn gingen
// ({ms, text}); pas bij het mappen worden het frames. Een frame dat over twee
// brokken verdeeld binnenkwam krijgt het moment van het brok waarin het af was.

// timedFrames zet brokken om in [{ms, frame}] in volgorde. Commentaarregels en
// frames die geen JSON zijn vallen weg, zoals in readSSE van de frontend.
export function timedFrames(chunks) {
  const out = []
  let buffer = ''
  for (const chunk of chunks) {
    buffer += chunk.text.replace(/\r\n?/g, '\n')
    let cut
    while ((cut = buffer.indexOf('\n\n')) >= 0) {
      const block = buffer.slice(0, cut)
      buffer = buffer.slice(cut + 2)
      const frame = parseBlock(block)
      if (frame) out.push({ ms: chunk.ms, frame })
    }
  }
  const tail = parseBlock(buffer)
  if (tail) out.push({ ms: chunks.at(-1)?.ms ?? 0, frame: tail })
  return out
}

export function sseFrames(text) {
  return timedFrames([{ ms: 0, text }]).map((f) => f.frame)
}

function parseBlock(block) {
  const data = block.split('\n').filter((l) => l.startsWith('data:')).map((l) => l.slice(5).replace(/^ /, '')).join('\n')
  if (!data) return null
  try {
    const frame = JSON.parse(data)
    return frame && typeof frame === 'object' ? frame : null
  } catch {
    return null
  }
}
