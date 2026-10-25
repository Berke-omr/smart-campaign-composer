# Test scenarios — briefs a marketer would really write

> Twenty end-to-end scenarios for trying the system by hand, through the API (`curl`) or the UI.
> Sources: campaign types and brief wording from desk research (Klaviyo, Insider, ikas, ideasoft,
> ticaret.gov.tr / İYS), turned into briefs and checked against the segment registry, the seed and the
> mock LLM. Rationale for the mock entries: [`DECISIONS.md`](../DECISIONS.md) D12.

## Before you start

1. Build and start the stack **in mock mode** from this branch (the scenario catalog is embedded in the
   api binary, so an older image does not know the new briefs):

   ```bash
   LLM_PROVIDER=mock docker compose up -d --build --wait
   curl -s localhost:8080/api/config     # must print {"llm":{"mode":"mock","model":null}}
   ```

   `.env` may hold a real key with `LLM_PROVIDER=auto`; without the override above, every brief is a paid call.
2. Run `make reseed` right before the session. It **deletes every user and campaign** and seeds 5,000 users
   again. Seed timestamps are fixed when the seed runs, while "in the last N days" is counted from now, so the
   audiences of time-based rules shrink day by day (the sample brief: 177 right after a reseed, 157 three days
   later). The numbers below are the ones right after a reseed.
3. A campaign freezes its numbers at its own `as_of` (creation time); approving later sends to exactly
   those people.

Briefs are matched to the mock catalog **exactly** (only whitespace and letter case may differ). Only S01–S05
are in the UI's Brief library; paste the others into the composer, or use the `curl` commands.

## Audience numbers

Right after `make reseed` (seed 42, 5,000 users). Measured on 2026-10-22 against the seed's own time; the
sample brief gives 177 / 99 / 133, the numbers the tests pin. Reachable = consent **and** contact data.

| Scenario | Rule | People | SMS reachable | Email reachable |
|---|---|---|---|---|
| S01 | İstanbul, cart ≤ 30 d, no purchase 30 d | 177 | 99 | 133 |
| S02 | İstanbul or Ankara, no purchase 90 d | 1,515 | 968 | 1,150 |
| S03 | cart ≤ 60 d | 2,053 | 1,276 | 1,529 |
| S04, S09, S16, S20 | everyone | 5,000 | 3,156 | 3,714 |
| S05 | İzmir, orders ≥ 5 | 79 | 45 | 49 |
| S06 | orders ≥ 1, no purchase 180 d | 1,241 | 764 | 940 |
| S07 | signed up ≤ 30 d, orders = 0 | 29 | 21 | 20 |
| S08 | orders ≥ 3 | 1,618 | 1,028 | 1,195 |
| S10 | total spent > 10,000 TL | 59 | 40 | 47 |
| S11 | total spent > 20,000 TL | 0 | 0 | 0 |
| S12 | İstanbul | 1,442 | 909 | 1,078 |
| S13 | Ankara or Eskişehir | 850 | 567 | 652 |
| S14 | language en | 519 | 318 | 399 |
| S15 | orders = 0, no cart add 180 d | 788 | 520 | 597 |
| S17 | purchased ≤ 30 d | 458 | 285 | 345 |
| S18, S19 | İstanbul, cart ≤ 30 d | 294 | 175 | 225 |

To measure them on the day (`make psql`, or `docker compose exec -T postgres psql -U scc -d scc`), with the
same semantics as the compiler (`not_within_last_days` includes "never"):

```sql
SELECT v.sc,
       count(*) FILTER (WHERE m) AS people,
       count(*) FILTER (WHERE m AND sms_opt_in AND phone IS NOT NULL) AS sms,
       count(*) FILTER (WHERE m AND email_opt_in AND email IS NOT NULL) AS email
FROM users CROSS JOIN LATERAL (VALUES
  ('S01', city = 'İstanbul' AND last_cart_add_at >= now() - interval '30 days'
          AND (last_purchase_at IS NULL OR last_purchase_at < now() - interval '30 days')),
  ('S02', city IN ('İstanbul', 'Ankara') AND (last_purchase_at IS NULL OR last_purchase_at < now() - interval '90 days')),
  ('S03', last_cart_add_at >= now() - interval '60 days'),
  ('S04 S09 S16 S20', true),
  ('S05', city = 'İzmir' AND order_count >= 5),
  ('S06', order_count >= 1 AND (last_purchase_at IS NULL OR last_purchase_at < now() - interval '180 days')),
  ('S07', created_at >= now() - interval '30 days' AND order_count = 0),
  ('S08', order_count >= 3),
  ('S10', total_spent > 10000),
  ('S11', total_spent > 20000),
  ('S12', city = 'İstanbul'),
  ('S13', city IN ('Ankara', 'Eskişehir')),
  ('S14', language = 'en'),
  ('S15', order_count = 0 AND (last_cart_add_at IS NULL OR last_cart_add_at < now() - interval '180 days')),
  ('S17', last_purchase_at >= now() - interval '30 days'),
  ('S18 S19', city = 'İstanbul' AND last_cart_add_at >= now() - interval '30 days')
) AS v(sc, m)
GROUP BY v.sc ORDER BY v.sc;
```

The api counts with the same rules (`POST /api/segments/preview` returned the same numbers for S01, S06, S07 and
S11 on 2026-10-22).

## How to run a scenario

**API.** Each scenario gives the body of `POST /api/campaigns`. Then, with `ID` from its response:

```bash
curl -s -X POST localhost:8080/api/campaigns -H 'content-type: application/json' --data-binary @- <<'EOF'
{"brief": "…", "channels": ["sms", "email"], "tone": "friendly"}
EOF
ID=…                                                              # "id" of the response
curl -N localhost:8080/api/campaigns/$ID/draft/stream             # draft.state / draft.delta … draft.done
curl -s -X PUT localhost:8080/api/campaigns/$ID/messages/sms \
  -H 'content-type: application/json' --data-binary @- <<'EOF'    # only when the scenario edits
{"text": "…"}
EOF
curl -s -X POST localhost:8080/api/campaigns/$ID/approve
curl -N localhost:8080/api/campaigns/$ID/events                   # delivery.snapshot every second … delivery.done
```

An email edit sends both fields: `{"subject": "…", "body": "…"}`.

**UI** (http://localhost:8080). New campaign → type or pick the brief, choose the channels and the tone →
Generate → the draft screen shows the audience card (rule lines, people, reachable and excluded per channel,
"Not applied: …" for unsupported criteria) and the message cards streaming → Edit on a card, Save →
Review & approve → the confirm dialog (final numbers) → Send messages → the live dashboard.

**Approval answer.** `200 {"campaign_id": ID, "status": "sending", "approved_at": "…", "deliveries": {"sms": N,
"email": M, "total": N+M}}`, where N and M are the reachable numbers of the selected channels (0 for a channel
that is not selected). Approving again returns the same body and writes nothing.

**Dashboard (every send).** Per-channel progress up to the delivery total, the sends-per-second chart under the
dashed 100/s line, and "Recent issues" from the mock provider's faults (about 3% `503`, 3% `429`, 0.5% slow
answers that time out, all retried; about 1% of recipients rejected for good). The campaign ends **Completed**
with a few failed messages; a big send shows them in every run.

---

## Scenarios

Categories: **1** fully supported · **2** partly supported (`unsupported_criteria`) · **3** nothing in the brief
can be expressed · **4** edge case. Every draft below is valid on the first attempt
(`writing → validating → valid`) unless the scenario says otherwise.

| No | Priority | Cat. | Type | Title | Channels | Tone | Mock source |
|---|---|---|---|---|---|---|---|
| S01 | P2 | 1 | Cart abandonment | Cart reminder — İstanbul | SMS + email | friendly | example `cart-reminder-istanbul` |
| S02 | P2 | 1 | Win-back | Two-city win-back | SMS + email | friendly | example `winback-90-days` |
| S03 | P1 | 2 | Seasonal | Gender flagged, not guessed | SMS | playful | example `new-season-sms` |
| S04 | P1 | 1, 4 | Weekend sale | Big send, consent, subject at the limit | email | professional | example `weekend-sale-email` |
| S05 | P1 | 1, 4 | VIP | Repair on screen, approve twice | SMS + email | friendly | example `vip-thanks-izmir` |
| S06 | P1 | 1 | Win-back | Buyers only | SMS + email | friendly | `winback-180-buyers` |
| S07 | P2 | 1, 4 | Welcome | New members, small audience | SMS + email | friendly | `welcome-new-members` |
| S08 | P1 | 2 | VIP | Time-windowed spend not approximated | SMS + email | professional | `vip-yearly-spend` |
| S09 | P1 | 3 | Seasonal | Age and gender only | SMS + email | friendly | `mothers-day-unsupported` |
| S10 | P2 | 1 | High spenders | Lifetime spend over 10,000 TL | email | professional | `high-spenders-10k` |
| S11 | P1 | 4 | High spenders | Zero audience | email | professional | `zero-audience-20k` |
| S12 | P2 | 2 | City | District not expressible | SMS + email | friendly | `store-opening-istanbul` |
| S13 | P2 | 1 | City | City written without Turkish letters | SMS | friendly | `same-day-delivery-ascii` |
| S14 | P2 | 1 | Language | English speakers | email | professional | `english-speakers` |
| S15 | P2 | 1 | Long inactive | Never ordered, cart idle | SMS + email | friendly | `never-ordered-dormant` |
| S16 | P2 | 3 | Long inactive | Engagement criteria only | email | friendly | `engagement-unsupported` |
| S17 | P1 | 4 | Flash sale | Every content error code on edit | SMS | urgent | `flash-sale-today` |
| S18 | P1 | 4 | Edge | Needs manual edit, 409, then fixed | SMS + email | friendly | `[test]` `fault-long-sms` |
| S19 | P2 | 4 | Edge | Refusal, timeout, emoji repair | varies | friendly / playful | `[test]` `fault-refusal`, `fault-timeout`, `fault-emoji` |
| S20 | P1 | 4 | Weekend sale | Dashboard while the api restarts | email | professional | example `weekend-sale-email` |

### S01 · Cart reminder — İstanbul (P2, cat. 1)

```json
{"brief": "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun.", "channels": ["sms", "email"], "tone": "friendly"}
```

- **Rule:** all of: City is İstanbul · Added to cart in the last 30 days · No purchase in the last 30 days. No warning.
- **Audience:** 177 people; SMS 99 (78 excluded), email 133 (44 excluded); 232 messages.
- **Drafts:** SMS and email valid; the SMS is valid while the email is still streaming.
- **Approve:** `200`, deliveries 99 + 133 = 232. **Dashboard:** both channels to Completed in a few seconds.

### S02 · Two-city win-back (P2, cat. 1)

```json
{"brief": "İstanbul veya Ankara'da yaşayıp son 90 gündür alışveriş yapmamış kullanıcılara bizi özlediniz mi mesajı gönder.", "channels": ["sms", "email"], "tone": "friendly"}
```

- **Rule:** City is one of İstanbul, Ankara · No purchase in the last 90 days (people who never bought are included).
- **Audience:** 1,515; SMS 968, email 1,150.
- **Approve:** `200`, 968 + 1,150 = 2,118. **Dashboard:** about 10–12 s per channel at the 100/s line.

### S03 · Gender flagged, not guessed (P1, cat. 2)

```json
{"brief": "Son 60 günde sepete ürün ekleyen kadın kullanıcılara yeni sezon ürünlerini SMS ile duyur.", "channels": ["sms"], "tone": "playful"}
```

- **Rule:** Added to cart in the last 60 days. **Unsupported:** `kadın kullanıcılar`; the audience card and the confirm
  dialog show "Not applied: kadın kullanıcılar".
- **Audience:** 2,053; SMS 1,276 (777 excluded). Only the SMS card.
- **Approve:** `200`, deliveries `{"sms": 1276, "email": 0}`. **Dashboard:** SMS only.

### S04 · Big send, consent, subject at the limit (P1, cat. 1 + 4)

```json
{"brief": "Tüm email abonelerine hafta sonuna özel %20 indirim duyurusu yap. Resmi bir dil kullan.", "channels": ["email"], "tone": "professional"}
```

- **Rule:** "All users" (no conditions). **Audience:** 5,000 people; email 3,714, **1,286 excluded** for lack of consent
  or an address.
- **Edits** (`PUT …/messages/email`; the worst case fills `{{first_name}}` = "Muhammed Emin", `{{last_name}}` =
  "Montgomery-Fitzgerald"; check `audience.longest_values.email` first). Body for every edit:
  `"Sayın {{first_name}} {{last_name}},\n\nHafta sonu boyunca tüm ürünlerde %20 indirim sizleri bekliyor.\n\nBu fırsatı kaçırmamanızı dileriz.\n\nSaygılarımızla,\nMağaza Ekibi"`

  | Subject | Template / worst case | Answer |
  |---|---|---|
  | `Sayın {{last_name}}, hafta sonuna özel %20 indirim fırsatı sizi bekliyor` | 72 / **80** | `200` (exactly at the limit) |
  | `Sayın {{first_name}} {{last_name}}, hafta sonuna özel %20 indirim sizi bekliyor` | 79 / **86** | `422` `too_long` ("…Shorten it by at least 6 characters.") |
  | `Hafta sonuna özel\n%20 indirim` | – | `422` `multiline` |
  | `Sayın {{ first_name }}, hafta sonuna özel %20 indirim` | – | `422` `malformed_variable` ("write it exactly as {{first_name}}, without spaces") |

  The UI counter shows the worst case (80 / 80, then 86 / 80 "6 over limit") before saving, the same number the
  backend checks. A rejected edit leaves the stored message unchanged.
- **Approve:** `200`, deliveries `{"sms": 0, "email": 3714, "total": 3714}`.
- **Dashboard:** about 37 s or more at the 100/s line; Recent issues fill with "Provider busy · retrying",
  "Provider error · retrying", "No answer · retrying" and "Rejected · won't retry". Completed with 3,714 done,
  a few dozen failed (31 in the P6 run).

### S05 · Repair on screen, approve twice (P1, cat. 1 + 4)

```json
{"brief": "İzmir'de en az 5 siparişi olan müşterilere teşekkür mesajı ve %15 indirim gönder.", "channels": ["sms", "email"], "tone": "friendly"}
```

- **Rule:** City is İzmir · Order count is at least 5. **Audience:** 79; SMS 45, email 49.
- **Drafts:** SMS `writing1 validating1 fixing2 validating2 valid2` (the first answer is over 160; the card shows
  "Fixing… · attempt 2 of 3" and keeps the old text until the new one streams); email valid.
- **Approve:** `200`, 45 + 49 = 94. **Approve again** (curl twice, or double-click): the same body, no new
  deliveries; `SELECT count(*) FROM deliveries WHERE campaign_id = <ID>` stays 94.

### S06 · Win-back, buyers only (P1, cat. 1)

```json
{"brief": "Daha önce en az 1 kez alışveriş yapmış ama son 180 gündür alışveriş yapmayan müşterilere 'seni özledik' mesajı ve %20 kupon gönder.", "channels": ["sms", "email"], "tone": "friendly"}
```

- **Rule:** Order count is at least 1 · No purchase in the last 180 days.
- **Audience:** 1,241; SMS 764, email 940. Without the order condition it would be 3,052 (people who never bought
  match "no purchase"): the condition is what keeps them out.
- **Approve:** `200`, 764 + 940 = 1,704.

### S07 · New members, small audience (P2, cat. 1 + 4)

```json
{"brief": "Son 30 günde üye olup henüz hiç sipariş vermemiş yeni üyelerimize ilk siparişe özel %15 hoş geldin kuponu gönder.", "channels": ["sms", "email"], "tone": "friendly"}
```

- **Rule:** Signed up in the last 30 days · Order count is 0. **Audience:** 29; SMS 21 (8 excluded), email 20 (9 excluded).
- **Approve:** `200`, 21 + 20 = 41; the send ends within a second or two.
- The seed has few recent sign-ups by design (sign-up dates come before each user's activity); "son 7 günde" gives
  8 people.

### S08 · Time-windowed spend not approximated (P1, cat. 2)

```json
{"brief": "Son 1 yılda 5.000 TL üzeri harcama yapan ve en az 3 siparişi olan müşterilere VIP teşekkür mesajı gönder.", "channels": ["sms", "email"], "tone": "professional"}
```

- **Rule:** Order count is at least 3. **Unsupported:** `son 1 yılda 5.000 TL üzeri harcama` (spend is stored for the
  whole lifetime only).
- **Audience:** 1,618; SMS 1,028, email 1,195. Using lifetime spend instead (`total_spent > 5000`) would give 442:
  that would be an approximation the prompt forbids.
- **Approve:** `200`, 1,028 + 1,195 = 2,223.

### S09 · Age and gender only (P1, cat. 3)

```json
{"brief": "35 yaş üstü kadın müşterilerimize Anneler Günü'ne özel %20 indirim gönder.", "channels": ["sms", "email"], "tone": "friendly"}
```

- **Rule:** "All users". **Unsupported:** `35 yaş üstü`, `kadın müşteriler`. Not a `422`: the rule is valid, it
  just has no conditions.
- **Audience:** 5,000; SMS 3,156, email 3,714. The warning plus "All users" is the signal not to send.
- **Expected:** the marketer does not approve. If approved: `200`, 3,156 + 3,714 = 6,870 messages, and the confirm
  dialog repeats "Not applied: …".

### S10 · Lifetime spend over 10,000 TL (P2, cat. 1)

```json
{"brief": "Toplam harcaması 10.000 TL'nin üzerinde olan müşterilerimize özel VIP indirim kodu gönder.", "channels": ["email"], "tone": "professional"}
```

- **Rule:** Total spent (TRY) is more than 10000. **Audience:** 59; email 47.
- The email does not invent a code (the prompt forbids codes the brief does not give).
- **Approve:** `200`, deliveries `{"sms": 0, "email": 47, "total": 47}`.

### S11 · Zero audience (P1, cat. 4)

```json
{"brief": "Toplam harcaması 20.000 TL'yi geçen müşterilere özel davet maili gönder.", "channels": ["email"], "tone": "professional"}
```

- **Rule:** Total spent (TRY) is more than 20000. The seed's highest spend is 19,326.48 TL, so the audience is
  **0** whatever the date.
- **Drafts:** the email is still written and valid (the worst case uses the variables' fallbacks); status Ready.
- **Approve:** `422 {"error": "no one in this audience can be reached on the selected channels"}`; the campaign stays
  Ready and no dashboard opens.

### S12 · District not expressible (P2, cat. 2)

```json
{"brief": "İstanbul Avrupa yakasındaki müşterilerimize Beylikdüzü'nde açılan yeni mağazamızı duyur, açılış haftasına özel %10 indirim.", "channels": ["sms", "email"], "tone": "friendly"}
```

- **Rule:** City is İstanbul. **Unsupported:** `Avrupa yakası`.
- **Audience:** 1,442; SMS 909, email 1,078. **Approve:** `200`, 1,987.

### S13 · City written without Turkish letters (P2, cat. 1)

```json
{"brief": "Ankara ve Eskisehir'deki musterilerimize ayni gun teslimat basladi, SMS ile haber ver.", "channels": ["sms"], "tone": "friendly"}
```

- **Rule:** City is one of Ankara, Eskişehir: the value is the registry's spelling. A rule with "Eskisehir" is
  rejected (`invalid_value`, listing the allowed cities).
- **Audience:** 850; SMS 567. The SMS uses `{{city}} için` (no suffix on a variable) and is written with Turkish letters.
- **Approve:** `200`, deliveries `{"sms": 567, "email": 0}`.

### S14 · English speakers (P2, cat. 1)

```json
{"brief": "İngilizce konuşan müşterilerimize İngilizce bir mail ile yeni koleksiyonu tanıt.", "channels": ["email"], "tone": "professional"}
```

- **Rule:** Language is English. **Audience:** 519; email 399.
- **Drafts:** the mock writes the email in English. With Claude, the prompt says "write in the language of the
  brief" while the brief asks for English: see the real-model check below.
- **Approve:** `200`, 399.

### S15 · Never ordered, cart idle (P2, cat. 1)

```json
{"brief": "Üye olup hiç sipariş vermemiş ve son 6 aydır sepetine ürün eklememiş kullanıcılara ilk siparişe %15 indirim gönder.", "channels": ["sms", "email"], "tone": "friendly"}
```

- **Rule:** Order count is 0 · Nothing added to cart in the last 180 days ("6 ay" → 180 days; people who never added to
  a cart are included).
- **Audience:** 788; SMS 520, email 597. **Approve:** `200`, 1,117.

### S16 · Engagement criteria only (P2, cat. 3)

```json
{"brief": "Son 6 aydır maillerimizi açmayan ve uygulamaya giriş yapmayan kullanıcılara geri dönüş maili at.", "channels": ["email"], "tone": "friendly"}
```

- **Rule:** "All users". **Unsupported:** `son 6 aydır mail açmayanlar`, `son 6 aydır uygulamaya giriş yapmayanlar`.
  Neither is mapped to purchase or cart dates.
- **Audience:** 5,000; email 3,714. **Expected:** not approved (as S09).

### S17 · Every content error code on edit (P1, cat. 4)

```json
{"brief": "Sadece bugün geçerli %30 flash sale! Son 30 günde alışveriş yapan müşterilere SMS ile duyur, acil bir dil olsun.", "channels": ["sms"], "tone": "urgent"}
```

- **Rule:** Purchased in the last 30 days ("sadece bugün" is about the offer, not the audience). **Audience:** 458; SMS 285.
- **Edits** (`PUT …/messages/sms`, worst case `{{first_name}}` = "Muhammed Emin"):

  | `text` | Worst case | Answer |
  |---|---|---|
  | `{{first_name}}, sadece bugün geçerli FLASH SALE başladı! Tüm ürünlerde %30 indirim seni bekliyor. Kampanya gece yarısı bitiyor, sakın kaçırma. Kod: FLASH30. Hemen alışverişe başla!` | 179 / 160 | `422` `too_long` (shorten by at least 19) |
  | `{{first_name}}, sadece bugün tüm ürünlerde %30 indirim! 🔥 Kod: FLASH30` | 69 | `422` `emoji` |
  | `{{first_name}}, sadece bugün tüm ürünlerde %30 indirim! Kod: {{coupon_code}}` | 75 | `422` `unknown_variable` |
  | `{{first_name}}, sadece bugün tüm ürünlerde %30 indirim! Gece yarısına kadar geçerli. Kod: FLASH30` | 96 / 160 | `200`, source `edited` |

- **Approve:** `200`, deliveries `{"sms": 285, "email": 0}`.

### S18 · Needs manual edit, 409, then fixed (P1, cat. 4)

```json
{"brief": "[test] Every SMS answer is 200 characters long, so the SMS needs a manual edit.", "channels": ["sms", "email"], "tone": "friendly"}
```

- **Rule:** City is İstanbul · Added to cart in the last 30 days. **Audience:** 294; SMS 175, email 225.
- **Drafts:** SMS `writing1 validating1 fixing2 validating2 fixing3 validating3 needs_manual_edit3`; email valid;
  status Draft.
- **Approve before the edit:** `409 {"error": "campaign is not ready to send: every selected channel needs a valid message"}`.
- **Edit** the SMS to the last text of S17 → `200`, status Ready. **Approve:** `200`, 175 + 225 = 400.

### S19 · Refusal, timeout, emoji repair (P2, cat. 4)

Same rule and audience as S18.

| Brief (exact) | Channels | Tone | Drafts | Then |
|---|---|---|---|---|
| `[test] The SMS is declined twice (final); the email is declined once and then written.` | SMS + email | friendly | SMS `writing1 fixing2 needs_manual_edit2` (the refusal's category shown, "No text yet."); email valid on attempt 2 | approve `409`; edit the SMS → `200`; approve `200` |
| `[test] The email never answers, so every attempt times out; the SMS is fine.` | SMS + email | friendly | email `writing1 fixing2 fixing3 needs_manual_edit3` (each attempt waits for its timeout); SMS valid | approve `409`; edit the email → `200`; approve `200` |
| `[test] The first SMS ends with an emoji; the repair removes it.` | SMS | playful | SMS `writing1 validating1 fixing2 validating2 valid2`, the problem names the emoji | approve `200` |

### S20 · Dashboard while the api restarts (P1, cat. 4)

Brief, channels and tone of S04 (a new campaign).

1. Approve, open the dashboard (or `curl -N …/events`).
2. At about 30% done: `docker compose stop api`. Within about a second: "Reconnecting…", the last numbers stay on
   screen ("Showing the last update from …"). The worker keeps sending meanwhile.
3. About 10 s later: `docker compose start api`. A few `502`s while it starts, then live again with a higher
   `done`; the numbers never go down and the page is not reloaded.
4. Completed with `done` = 3,714 and `SELECT count(*) FROM deliveries WHERE campaign_id = <ID>` = 3,714 (one
   delivery per person, no duplicates).

---

## Real-model check

The mock answers above encode what Claude is expected to do. For the briefs where that is a judgement call, one
segment call each confirms it (`POST /api/campaigns` in anthropic mode, without opening the draft stream):
S08, S09, S12, S13, S15, S16. **Status:** not run yet.

**Run through the UI on 2026-10-26** (Claude Sonnet 5.5, `claude-sonnet-5-5`, after a reseed; read back from the
`campaigns` and `campaign_messages` tables):

| Scenario | Rule from Claude | Unsupported | Messages |
|---|---|---|---|
| S01 | city eq İstanbul · cart within 30 · purchase not within 30 (same as the mock) | none | SMS and email valid on attempt 1 |
| S03 | cart within 60 (same as the mock) | `kadın kullanıcılar (cinsiyet)` (the mock says `kadın kullanıcılar`) | SMS valid on attempt 1 |
| S17 | purchase within 30 (same as the mock) | none | SMS valid on attempt 1, urgent tone |

No difference in the rules; the only difference is the wording of the unsupported criterion. The six briefs above
are still unchecked against Claude.

## Coverage

| Type | Cat. 1 | Cat. 2 | Cat. 3 | Cat. 4 |
|---|---|---|---|---|
| Cart abandonment | S01 | | | S18, S19 |
| Win-back | S02, S06 | | | |
| VIP / loyalty | S05 | S08 | | S05 (approve twice) |
| New-customer welcome | S07 | | | S07 (small audience) |
| Seasonal | | S03 | S09 | |
| City | S13 | S12 | | |
| Language | S14 | | | |
| High spenders | S10 | | | S11 (zero audience) |
| Long inactive | S15 | | S16 | |
| Weekend / flash sale | S04 | | | S04 (big send, limits), S17, S20 |

| Edge case | Scenario |
|---|---|
| Zero audience (`422` at approval) | S11 |
| Consent exclusions | S04, S07, every audience card |
| Over-limit edit (`too_long`) | S04, S17 |
| `emoji` / `unknown_variable` / `malformed_variable` / `multiline` | S17, S19 / S17 / S04 / S04 |
| Needs manual edit (length, refusal, timeout) | S18, S19 |
| `409` before every channel is valid | S18, S19 |
| Approve twice (idempotent) | S05 |
| Big send and live chart (≥ 3,000 messages) | S04, S20 |
| Reconnecting… | S20 |
