// Records the backend's own length answers for many message templates, so a
// Vitest test can check that the counter on screen (src/logic/content.ts)
// computes exactly the backend's worst_case_length (DECISIONS.md D15).
//
//   LLM_PROVIDER=mock docker compose up -d --build --wait
//   npm run record:worst-case            # writes src/logic/__tests__/fixtures/worst-case.recorded.json
//
// It needs the running stack and refuses to run unless the api uses the mock LLM,
// so recording never costs a model call. It creates a few draft campaigns and
// PUTs each template as the SMS text and as the email subject. Rejected edits
// (422) store nothing; the report comes back either way.

import { writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const base = process.env.API_URL ?? 'http://localhost:8080'
const out = new URL('../src/logic/__tests__/fixtures/worst-case.recorded.json', import.meta.url)

async function call(method, path, body) {
  const init = { method }
  if (body !== undefined) {
    init.headers = { 'Content-Type': 'application/json' }
    init.body = JSON.stringify(body)
  }
  const res = await fetch(base + path, init)
  return { status: res.status, json: await res.json() }
}

// Deterministic random numbers (mulberry32), so a re-recording sends the same templates.
function rng(seed) {
  let a = seed
  return () => {
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

// Edge cases of the backend's parser and counter, then random mixes of the same pieces.
const handPicked = [
  '',
  'Merhaba {{first_name}}, sepetindeki ürünler seni bekliyor!',
  '{{first_name}} {{last_name}} {{city}}',
  '{{last_name}}{{last_name}}{{last_name}}',
  'Merhaba {{ first_name }}',
  'Merhaba {{First_Name}}',
  'Merhaba {{name}} ve {{first_name}}',
  'Merhaba {{first_name}',
  'Merhaba first_name}} ve {{city}}',
  '}}{{first_name}}{{',
  '{{{first_name}}}',
  '{{}}',
  'İndirim {{%30}} {{city}}',
  'Tek parantez {first_name} değişken değildir',
  'Toplam 𝟑𝟎 indirim {{first_name}}',
  'é ve ş ğ ı İ ü ö ç â',
  'a'.repeat(147) + '{{first_name}}',
  'a'.repeat(148) + '{{first_name}}',
  'ş'.repeat(160),
  'ğ'.repeat(161),
]
const pieces = [
  'a',
  'ş',
  'İ',
  ' ',
  '%30',
  '!',
  'ğü',
  'â',
  'é',
  '𝟑',
  '{{first_name}}',
  '{{last_name}}',
  '{{city}}',
  '{{',
  '}}',
  '{',
  '}',
  '{{ city }}',
  '{{name}}',
  '{{first_name}',
]
const random = rng(20261021)
const generated = []
for (let i = 0; i < 160; i++) {
  let t = ''
  for (let n = Math.floor(random() * 30); n > 0; n--)
    t += pieces[Math.floor(random() * pieces.length)]
  generated.push(t)
}
const templates = [...handPicked, ...generated]

const config = await call('GET', '/api/config')
if (config.json?.llm?.mode !== 'mock') {
  console.error(
    `refusing to record: the api runs with ${JSON.stringify(config.json)}, not the mock LLM`,
  )
  process.exit(1)
}

// Two audiences with different longest values: the first and the last example
// brief that use both channels (İstanbul and İzmir with the mock catalog).
const { json: examples } = await call('GET', '/api/briefs/examples')
const both = examples.examples.filter(
  (e) => e.channels.includes('sms') && e.channels.includes('email'),
)
if (both.length < 2) throw new Error('need two example briefs with both channels')
const picked = [both[0], both[both.length - 1]]

const cases = []
for (const example of picked) {
  const created = await call('POST', '/api/campaigns', {
    brief: example.brief,
    channels: ['sms', 'email'],
    tone: example.tone,
  })
  if (created.status !== 201)
    throw new Error(`create: ${created.status} ${JSON.stringify(created.json)}`)
  const id = created.json.id
  for (const template of templates) {
    for (const [channel, body, field] of [
      ['sms', { text: template }, 'text'],
      ['email', { subject: template, body: 'b' }, 'subject'],
    ]) {
      const res = await call('PUT', `/api/campaigns/${id}/messages/${channel}`, body)
      const report = res.status === 200 ? res.json.message.report : res.json.report
      if (!report) throw new Error(`no report: ${res.status} ${JSON.stringify(res.json)}`)
      const length = report.lengths.find((l) => l.field === field)
      cases.push({
        channel,
        field,
        template,
        longest_values: report.longest_values,
        template_length: length.template_length,
        worst_case_length: length.worst_case_length,
        valid: report.valid,
      })
    }
  }
}

writeFileSync(
  out,
  JSON.stringify(
    {
      note: 'Recorded from the live api by scripts/record-worst-case.mjs; do not edit by hand.',
      recorded_at: new Date().toISOString(),
      cases,
    },
    null,
    1,
  ) + '\n',
)
console.log(`${cases.length} answers recorded in ${fileURLToPath(out)}`)
