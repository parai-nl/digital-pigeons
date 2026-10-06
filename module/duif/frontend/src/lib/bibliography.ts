// Citation completeness is bibliographic metadata, not evidence grounding or
// a scientific reliability score. The original reference is shown verbatim.
export function bibliographyNotice(ref: { citation?: string; citationConfidence?: string; citationMissing?: string } | undefined, lang: string): string {
  const nl = lang === 'nl'
  if (ref?.citationMissing?.trim()) return nl
    ? 'De bronvermelding is onvolledig en moet worden gecontroleerd. Dit is geen oordeel over de kwaliteit van het onderzoek.'
    : 'The reference is incomplete and needs checking. This is not an assessment of the research quality.'
  if (ref?.citationConfidence === 'medium' || ref?.citationConfidence === 'low') return nl
    ? 'Deze bibliografische gegevens moeten nog worden gecontroleerd. Dit staat los van de onderbouwing van de uitspraak.'
    : 'These bibliographic details still need checking. This is separate from the evidence supporting the claim.'
  if (!ref?.citation?.trim()) return nl
    ? 'Een volledige bronvermelding is hier niet beschikbaar. Publicatiegegevens worden niet uit de titel afgeleid.'
    : 'A full reference is not available here. Publication details are not inferred from the title.'
  return ''
}
