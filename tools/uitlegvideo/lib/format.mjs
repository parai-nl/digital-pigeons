// Beeldformaten van de uitlegvideo. Een formaat is het venster van
// de app (CSS-pixels), de schaal waarmee Chromium het tekent en de maat van
// de video; venster × schaal is de videomaat. Zo staat altijd de hele app in
// beeld, zonder uitsnede, en kiest het formaat alleen hoe groot de app oogt.
export function resolveFormat(config, name) {
  const formats = config.formats ?? {}
  const key = name ?? config.defaultFormat ?? Object.keys(formats)[0]
  const f = formats[key]
  if (!f) throw new Error(`onbekend formaat ${key}; kies uit ${Object.keys(formats).join(', ')}`)
  const { viewport, scale, out } = f
  if (Math.abs(viewport.width * scale - out.width) > 1 || Math.abs(viewport.height * scale - out.height) > 1) {
    throw new Error(`formaat ${key}: ${viewport.width}×${viewport.height} × ${scale} is geen ${out.width}×${out.height}`)
  }
  if (out.width % 2 || out.height % 2) throw new Error(`formaat ${key}: H.264 vraagt een even videomaat`)
  const isDefault = key === (config.defaultFormat ?? Object.keys(formats)[0])
  return { name: key, viewport, scale, out, suffix: isDefault ? '' : `-${key}` }
}
