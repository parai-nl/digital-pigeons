export type GestureKind = 'peck' | 'ruffle'
export type GestureVariant = 'auto' | GestureKind
export type GestureChoice = { kind: GestureKind; rate: number }

/** Each pair contains both gestures; only the next event consumes randomness. */
export function createGestureSequence(random: () => number = Math.random) {
  let bag: GestureKind[] = []
  const draw = () => {
    const value = random()
    return Number.isFinite(value) ? Math.max(0, Math.min(.999999, value)) : .5
  }
  return (variant: GestureVariant = 'auto'): GestureChoice => {
    if (variant === 'auto' && !bag.length) bag = draw() < .5 ? ['peck', 'ruffle'] : ['ruffle', 'peck']
    return { kind: variant === 'auto' ? bag.shift()! : variant, rate: .9 + draw() * .2 }
  }
}
