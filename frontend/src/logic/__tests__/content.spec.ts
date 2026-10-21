import { describe, expect, it } from 'vitest'
import type { Values } from '@/api/types'
import {
  charCount,
  counterLevel,
  insertAt,
  parseTemplate,
  render,
  sampleUser,
  worstCaseLength,
} from '../content'
import recorded from './fixtures/worst-case.recorded.json'

// The longest values of the case's sample audience (backend content tests).
const sampleLongest: Values = {
  first_name: 'Muhammed Emin',
  last_name: 'Montgomery-Fitzgerald',
  city: 'İstanbul',
}

describe('the counter computes what the backend enforces', () => {
  // 720 answers of the live api (scripts/record-worst-case.mjs): edge cases of
  // the parser, Turkish letters, combining marks, characters outside the BMP,
  // two audiences, SMS text and email subject.
  it(`equals worst_case_length for all ${recorded.cases.length} recorded answers`, () => {
    const mismatches = recorded.cases
      .map((c) => ({
        template: c.template,
        backend: c.worst_case_length,
        ui: worstCaseLength(c.template, c.longest_values as Values),
        templateBackend: c.template_length,
        templateUi: charCount(c.template),
      }))
      .filter((m) => m.ui !== m.backend || m.templateUi !== m.templateBackend)
    expect(mismatches).toEqual([])
  })

  it('records enough cases to matter', () => {
    const cases = recorded.cases
    expect(cases.length).toBeGreaterThanOrEqual(600)
    // The worst case differs from the plain template length in most of them, and
    // some contain characters that UTF-16 counts twice.
    expect(cases.filter((c) => c.worst_case_length !== c.template_length).length).toBeGreaterThan(
      300,
    )
    expect(
      cases.filter((c) => [...c.template].length !== c.template.length).length,
    ).toBeGreaterThan(100)
    expect(new Set(cases.map((c) => JSON.stringify(c.longest_values))).size).toBeGreaterThanOrEqual(
      2,
    )
  })
})

describe('charCount', () => {
  it('counts code points, not UTF-16 units', () => {
    expect(charCount('ş'.repeat(160))).toBe(160)
    expect(charCount('𝟑𝟎')).toBe(2)
    expect('𝟑𝟎'.length).toBe(4)
  })
})

describe('worstCaseLength', () => {
  it('fills each variable with the longest value of the audience', () => {
    // 147 literal characters + {{first_name}}: 161 as written, 160 rendered.
    expect(worstCaseLength('a'.repeat(147) + '{{first_name}}', sampleLongest)).toBe(160)
    expect(worstCaseLength('a'.repeat(148) + '{{first_name}}', sampleLongest)).toBe(161)
    expect(worstCaseLength('x'.repeat(40) + '{{last_name}} {{last_name}}', sampleLongest)).toBe(
      40 + 21 + 1 + 21,
    )
  })

  it('counts the fallback when it is longer than the data', () => {
    expect(worstCaseLength('Merhaba {{first_name}}', {})).toBe(charCount('Merhaba Müşterimiz'))
    expect(worstCaseLength('Merhaba {{first_name}}', { first_name: 'Ece' })).toBe(
      charCount('Merhaba Müşterimiz'),
    )
  })
})

describe('parseTemplate and render', () => {
  it('keeps text that is not a valid variable exactly as written', () => {
    expect(render('Merhaba {{name}} {{first_name}} {{', { first_name: 'Ece' })).toBe(
      'Merhaba {{name}} Ece {{',
    )
    expect(render('Merhaba {{ first_name }}', sampleLongest)).toBe('Merhaba {{ first_name }}')
    expect(render('Tek parantez {first_name}', sampleLongest)).toBe('Tek parantez {first_name}')
    expect(render('}}{{city}}', sampleLongest)).toBe('}}İstanbul')
  })

  it('splits a template into text and the three variables', () => {
    expect(parseTemplate('Merhaba {{first_name}}, {{city}} için')).toEqual([
      { kind: 'text', text: 'Merhaba ' },
      { kind: 'variable', variable: 'first_name' },
      { kind: 'text', text: ', ' },
      { kind: 'variable', variable: 'city' },
      { kind: 'text', text: ' için' },
    ])
  })

  it('renders the fictional sample user with the audience city', () => {
    expect(render('{{first_name}} {{last_name}}, {{city}}', sampleUser(sampleLongest))).toBe(
      'Ayşe Yılmaz, İstanbul',
    )
    expect(sampleUser({}).city).toBe('İstanbul')
  })
})

describe('counterLevel', () => {
  it.each([
    [143, 160, 'ok'],
    [144, 160, 'near'],
    [160, 160, 'near'],
    [161, 160, 'over'],
    [71, 80, 'ok'],
    [72, 80, 'near'],
    [80, 80, 'near'],
    [81, 80, 'over'],
    [5000, 0, 'ok'],
  ] as const)('%i of %i is %s', (length, limit, level) => {
    expect(counterLevel(length, limit)).toBe(level)
  })
})

describe('insertAt', () => {
  it('replaces the selection and puts the caret after the insertion', () => {
    expect(insertAt('Merhaba , hoş geldin', 8, 8, '{{first_name}}')).toEqual({
      value: 'Merhaba {{first_name}}, hoş geldin',
      caret: 22,
    })
    expect(insertAt('Merhaba XXX!', 8, 11, '{{city}}')).toEqual({
      value: 'Merhaba {{city}}!',
      caret: 16,
    })
  })
})
