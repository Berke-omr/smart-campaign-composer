// Message length rules, ported from the backend's content package
// (backend/internal/content/template.go and validate.go, DECISIONS.md D15) so
// the counter on screen shows the very number the backend enforces:
//
//   - lengths are Unicode code points, like Go's utf8.RuneCountInString;
//     JavaScript's `str.length` counts UTF-16 units and would count "𝟑" twice;
//   - the limit applies to the worst case: every variable replaced by the
//     longest value among the channel's recipients (or its fallback when that
//     is longer);
//   - text that is not a valid variable ("{{ first_name }}", "{{name}}", an
//     unclosed "{{") is counted as written, exactly as the backend renders it.
//
// The backend stays the authority: every save is validated there again.
// A test compares these functions with answers recorded from the live api.

import type { Channel, Field, MessageContent, Values, Variable } from '@/api/types'
import { VARIABLES } from '@/api/types'

export const SMS_LIMIT = 160
export const SUBJECT_LIMIT = 80

/** The fields of a channel's message, in display order. */
export function fieldsOf(channel: Channel): Field[] {
  return channel === 'sms' ? ['text'] : ['subject', 'body']
}

/** The character limit of a field; 0 means none (the email body). */
export function fieldLimit(field: Field): number {
  return field === 'text' ? SMS_LIMIT : field === 'subject' ? SUBJECT_LIMIT : 0
}

/** Rendered when a recipient's value is empty (content.Fallbacks). */
export const FALLBACKS: Record<Variable, string> = {
  first_name: 'Müşterimiz',
  last_name: '',
  city: '',
}

export function placeholder(v: Variable): string {
  return `{{${v}}}`
}

/** Length in Unicode code points: the string iterator yields code points. */
export function charCount(s: string): number {
  return [...s].length
}

export type Token = { kind: 'text'; text: string } | { kind: 'variable'; variable: Variable }

function isVariable(name: string): name is Variable {
  return (VARIABLES as readonly string[]).includes(name)
}

/**
 * Splits a template into literal text and whitelisted variables, step by step
 * like content.parse: a "}}" before any "{{", an unclosed "{{", and anything
 * between braces that is not exactly a known variable stay literal text.
 */
export function parseTemplate(template: string): Token[] {
  const tokens: Token[] = []
  let rest = template
  while (rest !== '') {
    const open = rest.indexOf('{{')
    const closeBeforeOpen = rest.indexOf('}}')
    if (closeBeforeOpen >= 0 && (open < 0 || closeBeforeOpen < open)) {
      tokens.push({ kind: 'text', text: rest.slice(0, closeBeforeOpen + 2) })
      rest = rest.slice(closeBeforeOpen + 2)
      continue
    }
    if (open < 0) {
      tokens.push({ kind: 'text', text: rest })
      break
    }
    if (open > 0) tokens.push({ kind: 'text', text: rest.slice(0, open) })
    const close = rest.indexOf('}}', open + 2)
    if (close < 0) {
      tokens.push({ kind: 'text', text: rest.slice(open) })
      break
    }
    const name = rest.slice(open + 2, close)
    if (isVariable(name)) {
      tokens.push({ kind: 'variable', variable: name })
    } else {
      tokens.push({ kind: 'text', text: rest.slice(open, close + 2) })
    }
    rest = rest.slice(close + 2)
  }
  return tokens
}

/** Fills the variables (empty values use FALLBACKS); invalid text stays as written. */
export function render(template: string, values: Values): string {
  return parseTemplate(template)
    .map((t) => (t.kind === 'text' ? t.text : values[t.variable] || FALLBACKS[t.variable]))
    .join('')
}

/** Per variable, the longer of the audience's longest value and the fallback (worstValues). */
export function worstCaseValues(longest: Values): Record<Variable, string> {
  const worst = {} as Record<Variable, string>
  for (const v of VARIABLES) {
    const value = longest[v] ?? ''
    worst[v] = charCount(FALLBACKS[v]) > charCount(value) ? FALLBACKS[v] : value
  }
  return worst
}

/** The length the limit is checked against (FieldLength.worst_case_length). */
export function worstCaseLength(template: string, longest: Values): number {
  return charCount(render(template, worstCaseValues(longest)))
}

/** The clearly fictional person of the preview (content.SampleUser). */
export function sampleUser(longest: Values): Values {
  return { first_name: 'Ayşe', last_name: 'Yılmaz', city: longest.city || 'İstanbul' }
}

/** Renders every field of a message. */
export function renderMessage(message: MessageContent, values: Values): MessageContent {
  const out: MessageContent = {}
  for (const field of ['text', 'subject', 'body'] as const) {
    const value = message[field]
    if (value !== undefined) out[field] = render(value, values)
  }
  return out
}

export type CounterLevel = 'ok' | 'near' | 'over'

/** "Near limit" from 90% up to the limit itself (144/160, 72/80); over beyond it. */
export function counterLevel(length: number, limit: number): CounterLevel {
  if (limit <= 0) return 'ok'
  if (length > limit) return 'over'
  return length * 10 >= limit * 9 ? 'near' : 'ok'
}

/** Inserts text in place of the selection [start, end) and returns the caret after it. */
export function insertAt(
  value: string,
  start: number,
  end: number,
  text: string,
): { value: string; caret: number } {
  return { value: value.slice(0, start) + text + value.slice(end), caret: start + text.length }
}
