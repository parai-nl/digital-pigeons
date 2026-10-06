import { describe, expect, it } from 'vitest'
import { isReadableLabel, isReadableValue, readableLabel } from './readable'

describe('isReadableValue', () => {
  it('lets written text through', () => {
    expect(isReadableValue('Voeren houdt de kolonie groot en gezond.')).toBe(true)
    expect(isReadableValue('Volume and pages')).toBe(true)
    // Eén woord met een hoofdletter is een naam, en dus geschreven.
    expect(isReadableValue('Rie')).toBe(true)
    expect(isReadableValue('Stressrespons')).toBe(true)
  })

  it('keeps the keys of the import pipeline off the screen', () => {
    // Echte waarden uit de import: ruwe kolomnamen én ruwe waarden horen niet
    // op het scherm, en een nette kop erboven maakt een sleutel geen inhoud.
    expect(isReadableValue('wonen_en_leefomgeving')).toBe(false)
    expect(isReadableValue('algemeen')).toBe(false)
    expect(isReadableValue('persoonlijk')).toBe(false)
    expect(isReadableValue('respondent')).toBe(false)
    expect(isReadableValue('destillaat:0be06f8c-9f08-5307-9ac7-2f2410ce1b95')).toBe(false)
  })

  it('has no opinion about nothing', () => {
    expect(isReadableValue('')).toBe(false)
    expect(isReadableValue('   ')).toBe(false)
  })
})

describe('isReadableLabel', () => {
  it('keeps every name that a reader could have written', () => {
    expect(isReadableLabel('Stad en dier')).toBe(true)
    expect(isReadableLabel('Stressrespons')).toBe(true)
    // Een bronsleutel is kleingeschreven en zonder spatie, maar het is wél de
    // naam waaronder dat paper in de kennisbank staat.
    expect(isReadableLabel('lewald1987')).toBe(true)
    expect(isReadableLabel('Rie')).toBe(true)
  })

  it('refuses the shapes that only a pipeline produces', () => {
    expect(isReadableLabel('wonen_en_leefomgeving')).toBe(false)
    expect(isReadableLabel('thema_titel')).toBe(false)
    expect(isReadableLabel('destillaat:0be06f8c-9f08-5307-9ac7-2f2410ce1b95')).toBe(false)
    expect(isReadableLabel('thema:1')).toBe(false)
    expect(isReadableLabel('')).toBe(false)
    expect(isReadableLabel('   ')).toBe(false)
  })
})

describe('readableLabel', () => {
  it('puts the kind of node in the place of a key', () => {
    expect(readableLabel('Stad en dier', 'Theme')).toBe('Stad en dier')
    expect(readableLabel('  Stad en dier  ', 'Theme')).toBe('Stad en dier')
    expect(readableLabel('wonen_en_leefomgeving', 'Theme')).toBe('Theme')
    expect(readableLabel('', 'Thema')).toBe('Thema')
  })
})
