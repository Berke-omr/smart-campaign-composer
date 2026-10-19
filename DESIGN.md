# Composer — frontend design specification

Smart Campaign Composer lets a marketer write a brief, review the resulting audience and SMS/email drafts, edit and approve the content, then follow sending progress.

**Authority:** written requirements determine values, copy and counts. The original reference and `01-composer.png`, `02-draft-generating.png`, `08-live-sending.png` inform layout, composition and component shapes only. Never transcribe image text or numbers. This document is the frontend source of truth. Scope: light theme, desktop first at 1440px, usable at 1024px.

`Assumption:` identifies implementation details missing from the written requirements. It does not authorize additional product features.

## 1. Visual theme & atmosphere

Light, airy glassmorphism: cool background, broad aqua/sky-blue glows and a low peach glow. Translucent white surfaces have soft depth, fine white borders and an upper highlight. Navy anchors navigation; coral-to-pink highlights primary actions. Use generous whitespace, quiet outline icons and clear text. Blobs are atmospheric, never discrete illustrations.

## 2. Color tokens

These base colors come from the written design system. Alias values reference other tokens rather than introducing colors.

| CSS variable | Value | Role |
| --- | --- | --- |
| `--color-bg` | `#F4F7FB` | Page background |
| `--color-blob-aqua` | `#9FE8DA` | Top-left glow |
| `--color-blob-sky` | `#B9D4FF` | Center-right glow |
| `--color-blob-peach` | `#FFD3BE` | Bottom glow |
| `--color-white` | `#FFFFFF` | Glass base, inverse text, highlight |
| `--color-glass` | `rgb(255 255 255 / 55%)` | Standard glass surface |
| `--color-text` | `#0F172A` | Primary text |
| `--color-text-secondary` | `#5B6B82` | Supporting text |
| `--color-primary-start` | `#FF7A59` | Coral gradient start |
| `--color-primary-end` | `#FF4D8D` | Pink gradient end |
| `--color-navy` | `#111C33` | Active navigation |
| `--color-sms` | `#0EA5A4` | SMS dot, chart line, channel accent |
| `--color-email` | `#7C5CFF` | Email dot, chart line, channel accent |
| `--color-campaign-draft` | `#5B6B82` | Draft |
| `--color-campaign-ready` | `#F59E0B` | Ready |
| `--color-campaign-sending` | `#2F6BFF` | Sending |
| `--color-campaign-completed` | `#16A34A` | Completed; live connection dot |
| `--color-campaign-failed` | `#DC2626` | Failed; errors |
| `--color-message-queued` | `var(--color-campaign-draft)` | Queued |
| `--color-message-sending` | `var(--color-campaign-sending)` | Sending |
| `--color-message-sent` | `var(--color-campaign-completed)` | Sent |
| `--color-message-retrying` | `var(--color-campaign-ready)` | Retrying; warnings |
| `--color-message-failed` | `var(--color-campaign-failed)` | Failed |

**Assumption:** use the following derived tokens for surfaces, interactions and accessible text. They extend the supplied palette without sampling image colors. `family` below means `sms`, `email`, every `campaign-*` and every `message-*`; expand the patterns into actual CSS variables in the theme file.

| CSS variable / token pattern | Value | Role |
| --- | --- | --- |
| `--color-glass-border` | `rgb(255 255 255 / 65%)` | Glass border |
| `--color-glass-highlight` | `rgb(255 255 255 / 80%)` | Upper-edge highlight |
| `--color-glass-hover` | `rgb(255 255 255 / 72%)` | Hovered glass |
| `--color-field` | `rgb(255 255 255 / 35%)` | Inner input/preview surface |
| `--color-glass-opaque` | `var(--color-white)` | Opaque fallback |
| `--color-border-control` | `var(--color-text-secondary)` | Visible input/control boundary |
| `--color-focus` | `var(--color-navy)` | Focus outline |
| `--color-overlay` | `rgb(17 28 51 / 24%)` | Modal backdrop |
| `--color-track` | `rgb(91 107 130 / 12%)` | Progress track, skeleton |
| `--color-{family}-tint` | `color-mix(in srgb, var(--color-{family}) 12%, transparent)` | Soft pill/banner background |
| `--color-{family}-border` | `color-mix(in srgb, var(--color-{family}) 24%, transparent)` | Tinted pill/banner border |
| `--color-{family}-ink` | `color-mix(in srgb, var(--color-{family}) 50%, var(--color-text))` | Darker, hue-related text on tint |
| `--color-primary-aa-start` | `color-mix(in srgb, var(--color-primary-start) 65%, var(--color-navy))` | Accessible primary action start |
| `--color-primary-aa-end` | `color-mix(in srgb, var(--color-primary-end) 65%, var(--color-navy))` | Accessible primary action end |

Campaign and message pills use their family’s tint, border and ink, with the original hue for the dot/icon. Channel variable tokens use the same recipe with `sms` or `email`.

**Assumption:** where supplied pairings conflict with AA, enabled primary buttons use the derived AA gradient and white text; original coral/pink remain base brand tokens. The original gradient with white text does not reach 4.5:1. Keep gloss above the text area and verify composited contrast. See section 8.

## 3. Typography

Urbanist is for headings; Inter is for UI and message content, with `font-variant-numeric: tabular-nums` for metrics. JetBrains Mono is **only** for character counters. Preserve Turkish glyphs and use UTF-8.

**Assumption:** this type scale supplies unspecified sizes, weights and line heights. Tokenize all three; use `rem` sizes based on the browser default.

| Role / size token | Size / weight / line-height | Font | Use |
| --- | --- | --- | --- |
| Home hero / `--font-size-hero` | 64px / 600 / 72px | Urbanist | “Good morning, Ömer” |
| H1 / `--font-size-h1` | 36px / 700 / 44px | Urbanist | Campaign screen title |
| H2 / `--font-size-h2` | 24px / 700 / 32px | Urbanist | Sections, major card headings |
| H3 / `--font-size-h3` | 20px / 600 / 28px | Urbanist | Channel/card subheadings |
| Body / `--font-size-body` | 16px / 400 / 24px | Inter | Brief, content, navigation |
| Composer / `--font-size-compose` | 24px / 400 / 32px | Inter | Home brief input |
| Label / `--font-size-label` | 14px / 500 / 20px | Inter | Controls, chips, statuses |
| Caption / `--font-size-caption` | 12px / 400 / 16px | Inter | Notes, times, muted codes |
| Counter / `--font-size-counter` | 12px / 400 / 16px | JetBrains Mono | `current / limit` |

**Assumption:** font stacks are `--font-ui: Inter, system-ui, sans-serif`, `--font-heading: Urbanist, var(--font-ui)` and `--font-counter: "JetBrains Mono", monospace`. Statistics reuse H1 sizing with Inter. At constrained widths, wrap titles and reduce the home hero to H1 size.

## 4. Spacing, radii, shadows & glass

**Assumption:** spacing uses a 4px base: `--space-0` through `--space-8` = 0, 4, 8, 12, 16, 20, 24, 28, 32px; add `--space-10: 40px`, `--space-12: 48px`, `--space-16: 64px`. Default card padding and grid gap are `--space-6`; related label/value gaps use `--space-2`.

| Token | Value | Source / use |
| --- | --- | --- |
| `--radius-card` | 24px | Supplied; cards, sidebar, large panels |
| `--radius-field` | 18px | Supplied; inner fields and previews |
| `--radius-pill` | 9999px | Assumption: implementation of fully rounded buttons/chips |
| `--border-thin` | 1px | Supplied glass border width |
| `--blur-glass` | 24px | Assumption: strong glass blur |
| `--blur-blob` | 96px | Assumption: very soft atmospheric blobs |
| `--shadow-card` | `0 8px 32px rgb(17 28 51 / 6%)` | Assumption: soft depth |
| `--shadow-raised` | `0 12px 40px rgb(17 28 51 / 10%)` | Assumption: modal/hover depth |
| `--shadow-highlight` | `inset 0 1px 0 var(--color-glass-highlight)` | Assumption: top sheen |
| `--opacity-disabled` | 0.45 | Assumption: visibly disabled |
| `--focus-width`, `--focus-offset` | 2px, 4px | Assumption: visible focus |
| `--control-height`, `--icon-button-size` | 48px, 48px | Assumption: consistent controls |

Glass recipe: glass background; `backdrop-filter: blur(var(--blur-glass))` and matching `-webkit-backdrop-filter`; thin glass border; card radius; card shadow plus highlight. Inner fields use `--color-field`. Decorative layers stay behind content and ignore pointer events.

Without backdrop-filter support, use `--color-glass-opaque` on surfaces and fields; retain borders, radii and shadows. Essential control boundaries use the control-border token, not only a decorative white edge.

**Assumption — responsive layout tokens:** `--shell-inset: 20px`, `--sidebar-width: 256px`, `--sidebar-compact-width: 224px`, `--layout-gap: 24px`, `--breakpoint-compact: 1200px`. These are layout choices, not values sampled from the images.

- At 1440px: inset sidebar, flexible main column, search and account controls above the content. Draft uses a narrower brief/audience column and a wider message column. Dashboard uses two channel cards, then a wide chart and narrow issues card.
- From 1024px to below 1200px: use the compact sidebar; stack draft columns and chart/issues. Keep channel cards side by side only while their content fits, otherwise stack. Wrap legends, steps and top-bar controls; retain every control. Home campaign cards wrap into fewer columns.
- Use `min-width: 0`, fluid tracks and wrapping. Allow vertical page scroll; never fix the page to the screenshots’ height. No horizontal page overflow at 1024px. **Assumption:** a tall sidebar can scroll internally while remaining in view.

## 5. Components & states

**Assumption:** unspecified interaction mechanics below are implementation defaults. All enabled interactive components share a `:focus-visible` ring; hover never shifts layout.

| Component | Anatomy and states |
| --- | --- |
| Sidebar / nav item | Composer logo; Create → New campaign; Main → Campaigns, Audiences, Live sends, Templates; bottom logout. Nav uses field radius. Default glass, hover brighter glass, active navy with white text/icon and `aria-current="page"`; disabled cannot activate. New campaign active on composer/draft; Live sends active on dashboard. No additional destination screens. |
| Top bar | Rounded Search campaigns input, neutral glass info pill “Demo mode”, bell, settings, profile. Same order throughout; icon buttons use accessible names and shared interaction states. No notification/settings flows are specified. |
| Primary button | Rounded gradient, white label, top gloss; enabled uses AA gradient from section 2. Hover uses raised shadow, focus uses shared ring, disabled uses lower opacity and native disabled behavior. Loading keeps width/label, adds a spinner and prevents repeat submission. |
| Secondary glass button | Glass background, primary text, fine visible boundary. Hover brighter glass; shared focus, disabled and loading behavior. |
| Ghost button / text action | Transparent surface, readable text; hover adds a light glass surface or underline for text links. Same focus/disabled/loading behavior. Used for Edit brief and back links. |
| Chips | Fully rounded, glass/neutral surface; selected channel uses channel tint with dot + label. Condition chips wrap, joined by muted “and”. Action chips support hover/focus/disabled; static chips are not focusable. No extra dismiss/remove controls. |
| Status pill | Family tint/border/ink, dot or icon, explicit label; not a button. Campaign: Draft, Ready, Sending, Completed, Failed. Message: Queued, Sending, Sent, Retrying, Failed. Never substitute Delivered or Awaiting approval. |
| Text area / input | Visible label; glass field, field radius and padding. Normal visible boundary; focus ring; error boundary plus specific inline error/icon and `aria-invalid`. Disabled remains legible; readonly allows selection. Multiline content never covers actions. |
| Variable token pill | Atomic inline rounded token reading `first_name`, without brackets/braces; channel tint/border/ink. Align with the sentence baseline, keep punctuation outside the pill. In editable content it supports keyboard insertion and deletion; in previews it is a substituted value. |
| Character counter | JetBrains Mono, `current / limit`. Normal secondary text; near limit at ≥90% and ≤100% uses warning ink plus “Near limit”; over limit uses error ink plus an explicit characters-over-limit error. 144/160 and 72/80 start warning; 160 and 80 are valid. Do not rely on color alone. |
| Card | Heading, optional state/action, content, optional footer; card radius/padding/glass. Only interactive cards gain hover/focus treatment. Loading uses skeletons; empty/error states retain the card footprint. |
| Step indicator | Audience → Messages → Review → Send. Done: check + label; current: coral accent + darker readable label and `aria-current="step"`; upcoming: muted outline + label. Assumption: informational rather than clickable navigation. |
| Single progress bar | Rounded track and filled segment, visible percentage and completed/total text. Use exact data for fill width; round only the displayed percentage. Unknown progress is indeterminate and has no fabricated percentage. |
| Segmented progress bar | Order Sent → Sending → Queued → Retrying → Failed, with state colors and proportional widths. Keep tiny segments faithful to data; the adjacent legend always lists all five counts, including zero. Channel color belongs to the heading/gauge, not all state segments. |
| Modal | Glass dialog, tinted backdrop, heading/content, cancel/confirm actions. Trap/restore focus; Escape/cancel closes before confirmation. Confirmation shows loading and prevents repeat send; errors remain in the dialog. |
| Toast | Glass feedback with text/icon/tint for existing actions only. Provide dismiss, polite announcement, and persistent errors. No notification workflow. |
| Skeleton | Faint field/track-colored rounded lines; shimmer only while content is loading/writing. Match the shape of expected content, exclude skeletons from accessible text. Preserve completed text. |
| Banner | Inline glass/tinted notice with icon, short title/message and relevant existing action. Warning for skipped conditions; neutral/blue for reconnecting; error for server failure. Persistent issues remain until resolved; never replace the whole screen if only one card failed. |

**Assumption — motion:** `--duration-fast: 160ms`, `--duration-pulse: 1200ms`, `--duration-shimmer: 1600ms`, `--ease-ui: ease-out`. Use subtle opacity animation, not large movement. Disable shimmer, pulse and nonessential transitions under `prefers-reduced-motion: reduce`; retain static dots, labels and skeletons.

## 6. Screens & states

### Shared data and supplied layouts

Campaign: **Cart reminder — İstanbul**. Audience: **177 people**. SMS reachable **99**, not included without consent **78**. Email reachable **133**, not included without consent **44**. Total **232 channel messages**, not 232 unique people. People may be reachable on both channels.

**01 — Composer:** reference home composition: greeting, supporting approval reassurance, large brief field, SMS/Email chips, Tone: Friendly, Brief library, Generate, then four recent campaign cards. New campaign active; Demo mode in top bar. Cart reminder has 177 recipients and Sending at 62%; Welcome series has 3,920 recipients and Completed at 100%; Weekend flash sale has 2,105 recipients and Ready; Win-back 90 days has 860 recipients and Draft. These fixture values come from the written screen requests. Do not infer other progress values from image pixels.

**02 — Draft, generating:** back link “← New campaign”; campaign heading and gray Draft pill. Audience done, Messages current, Review and Send upcoming. Left: Brief and Audience cards. Right: SMS above Email. Bottom-right Review & approve is disabled while writing or validation is unfinished. Brief card has Edit brief. Audience conditions are City is İstanbul; Added to cart in the last 30 days; No purchase in the last 30 days. Show 177 people, channel reachability, and “Only people who opted in to a channel will receive it.”

Written content, with canonical variables:

```text
Brief
Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun.

SMS
Merhaba {{first_name}}, sepetindeki ürünler seni bekliyor! Bugüne özel %30 indirimle alışverişini tamamla: kmpny.co/sepet

Email subject
{{first_name}}, sepetindekilere %30 indirim seni bekliyor

Email body, visible while writing
Merhaba {{first_name}},

Sepetine eklediğin ürünler hâlâ seni bekliyor.
```

The partial email body is not a specified complete email. Do not invent its remaining content. Render draft counters from actual text, never from pixels or hard-coded illustrative counts.

**08 — Live, in progress:** Live sends active. Heading, blue Sending pill, green Live indicator, “Started 14:32:05 · 232 messages”. Overall: 62%, “143 of 232 done”. Channel cards side by side, then a wide chart and narrow Recent issues card.

| Channel | Sent | Sending | Queued | Retrying | Failed | Rate | Gauge limit |
| --- | ---: | ---: | ---: | ---: | ---: | --- | --- |
| SMS | 64 | 6 | 24 | 3 | 2 | 84 msg/s | limit 100/s |
| Email | 76 | 8 | 44 | 4 | 1 | 92 msg/s | limit 100/s |

**Assumption:** “done” includes Sent and terminal Failed: 64 + 76 + 2 + 1 = 143. Derive totals from channel data and update counts/progress together. Rate gauges represent rate ÷ 100/s, independent of message completion.

Chart title: “Sending speed · last 30 seconds”; smooth SMS teal and Email violet lines below a dashed “Limit 100/s” line. Do not invent historical samples or fabricate live data. Recent issues, in order:

| Message | Muted code | Time |
| --- | --- | --- |
| Provider busy · retrying in 2s | 429 | now |
| Provider error · retrying | 503 | 1s |
| Provider busy · retrying in 1s | 429 | 3s |
| Rejected · won't retry | 400 | 4s |

### Audience card

| State | Treatment and behavior |
| --- | --- |
| Normal | Readable condition chips joined by “and”, total people, per-channel reachable counts and consent note. No consent is never represented as reachable. |
| Skipped condition warning | Identify the omitted condition in a warning banner; keep applied conditions and resulting counts visible. Assumption: repeat the warning in review/confirmation; do not imply every condition was applied. |
| Zero audience | Show 0 people, zero channel reachability and “No people match this audience.” Keep conditions visible with Edit brief. Assumption: disable Review & approve/send until the audience is nonzero and a selected channel has reachable people. |

### Message cards

| State | Treatment and behavior |
| --- | --- |
| Writing | Channel heading, “Writing…” with pulsing dot, available text, cursor and two faint skeleton lines after partial body content. |
| Validating | Complete current text stays visible; blue spinner and “Validating…”. Review remains disabled. |
| Fixing | Keep the card layout; label “Fixing… · attempt 2 of 3”. Assumption: retain last available text while a correction is being prepared; never imply it has passed. |
| Valid | Check icon + “Valid”; stable editable text and current counters. No spinner or skeleton. |
| Needs manual edit | Label “Needs manual edit”; specific inline errors next to affected fields, visible error summary within card, counters preserved. Assumption: after the third unsuccessful attempt, stop automatic fixing and require edits. Never silently truncate text. |

### Review and edit

Reuse draft columns/cards; Messages done, Review current. Allow edits to SMS, email subject and body. Provide channel-tinted variable insert chips that insert `{{first_name}}` at the current caret. Recompute counters immediately: SMS ≤160; email subject ≤80. No email-body limit is specified. Show a sample-user preview alongside or beneath the editor without changing the saved draft.

**Assumption:** count Unicode code points in canonical draft text, including the literal stored variable syntax; use the same counting function everywhere. A sample-user substitution changes only the preview, not the draft counter. Variable expansion/length policy for real recipient values remains a product decision; do not claim the counter validates those expanded lengths.

**Assumption:** use a clearly labeled fictional sample user. Edits invalidate the affected card’s previous validation. Progression requires valid selected-channel drafts and passing fields/limits; keep audience warnings visible. Editing and preview never send messages.

### Confirm send modal

Show campaign title, audience summary/conditions, 177 people, selected-channel counts (99 SMS, 133 Email), total 232 messages for both channels, and content summary for each selected channel. State **“This can't be undone”** prominently before the final action.

**Assumption:** actions are “Cancel” and “Send messages”. Totals reflect selected channels. Only final confirmation starts sending; preserve content on failure and show a plain-language error. No additional send options are specified.

### Dashboard completion and connection

- **Completed:** same grid; Completed campaign pill; 100% and “232 of 232 done” once every message is terminal. Pending counts are zero. Use actual final Sent/Failed totals; do not project the in-progress sample into an invented final result. Keep chart and issue history. **Assumption:** stop rate animations and show actual final rate/connection state; message-level failures remain visible even if campaign state is Completed. Do not infer campaign Failed solely from one failed message.
- **Reconnecting:** keep the last known dashboard and counts, replace Live with “Reconnecting…”, and show a persistent banner. **Assumption:** mute the connection dot, label values as last updated, freeze the chart instead of drawing invented points, and resume from fresh data without resetting progress. Never use green Live while connection is unavailable.

### Generic states

- **Loading:** retain app chrome and layout; skeleton only the unavailable content, with a single understandable loading label. Disable actions that depend on missing data.
- **Empty:** a short explanation inside the relevant area and an existing relevant action, such as New campaign or Edit brief. No decorative illustration or fabricated campaigns.
- **Can't reach the server:** use that exact heading, plain-language supporting text, and preserve edits/last known results. **Assumption:** provide “Try again” to retry the failed operation; no stack traces, internal details or full-page blanking when usable content exists.

## 7. Content rules

- UI, labels, help and errors are English. Briefs and message content are Turkish. Render `ş, ğ, ı, İ, ü, ö, ç, â` correctly. Do not replace İstanbul with Istanbul or hâlâ with hala.
- No lorem ipsum, stock photos, added illustrations or invented feature copy. Keep language appropriate for marketers. Do not mention Claude, AI models, APIs, backend queues or implementation mechanisms. Queued is permitted as the requested message-state label.
- The explicit exceptions are the neutral **Demo mode** info pill and the requested dashboard issue messages with small muted error codes. Keep codes secondary; no technical explanations or internal traces elsewhere.
- Store variables as `{{first_name}}`; earlier `[first_name]` notation denotes the same variable. Render an inline rounded pill labeled `first_name`, tinted teal in SMS and violet in email, without showing delimiters. Previews substitute the sample user’s name. Do not introduce other variable names.
- Keep campaign statuses distinct from message states. Never label 232 messages as 232 people. Keep consent exclusions, reachability and channel totals consistent across every screen.
- Counters, chart data and live totals come from their current values. Hard-code only the explicitly supplied demo fixtures; do not simulate new results as real progress. No send occurs before explicit approval and final confirmation.

## 8. Accessibility

Target WCAG AA and **4.5:1 for text**. Test composited glass, gradient areas behind glyphs and opaque fallback. Verify derived ink/AA-action tokens; bright accents do not automatically pass. Meaningful controls/chart graphics also need discernible boundaries. Inactive controls have WCAG contrast exceptions; adjacent explanations must remain readable. See [W3C contrast guidance](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html).

- All actions work by keyboard; keep a clearly visible, unobscured focus ring. Label icon-only buttons and inputs. See [W3C focus guidance](https://www.w3.org/WAI/WCAG22/Understanding/focus-visible.html).
- Status pills always include text; use labels/icons as well as color for errors, warnings and steps. Give the chart an accessible text summary and channel legend; channel counts remain available outside the chart.
- Connect errors/counters to fields with `aria-describedby`. Announce validation changes and toasts appropriately. **Assumption:** use a polite live region for meaningful sending updates, avoiding announcements on every chart frame or individual rate change.
- Use semantic progress values and an accessible legend/summary for segmented bars. Modal focus is contained and restored. Disabled/loading actions cannot submit twice.
- Respect `prefers-reduced-motion` for shimmer, pulse, cursor effects and chart transitions; state labels must remain clear without animation. See [W3C motion guidance](https://www.w3.org/WAI/WCAG22/Understanding/animation-from-interactions.html).
- Check keyboard flows, 200% text zoom, Turkish glyph coverage and 1024px layout. Responsive reflow must not hide controls or important status text.

## 9. Do’s & don’ts

| Do | Don’t |
| --- | --- |
| Keep the original reference’s glass, glow, whitespace and outline icon character. | Accumulate style changes from generated images. |
| Use written copy/data and recompute derived values. | Copy misspelled text, counters or proportions from image pixels. |
| Reuse tokens, components and semantic states. | Add arbitrary colors, extra status names or one-off card shapes. |
| Preserve user edits, partial results and readable error context. | Erase useful content during validation or reconnecting. |
| Make approval, confirmation and progress unambiguous. | Send automatically, hide consent exclusions or invent delivery success. |

## 10. Notes for the Vue 3 coding agent

- Put all design tokens and derived families in one shared `:root` theme file. Never hard-code colors or sizes in components. Use shared build-time breakpoint tokens/custom media; native CSS variables cannot be used in media-query conditions.
- **Assumption:** use reusable Vue 3 components corresponding to section 5: AppSidebar, NavItem, TopBar, AppButton, Chip, StatusPill, TextInput/TextArea, VariableToken, CharacterCounter, GlassCard, StepIndicator, ProgressBar, SegmentedProgress, ConfirmSendModal, Toast, Skeleton and Banner. Compose channel/message/audience cards from them; do not duplicate their state styling per screen.
- Keep campaign status, message state, channel, validation state and connection state separate. A campaign can be Sending while one channel message is Retrying or Failed. Loading is not a campaign status. Use explicit component props/state variants and shared color mappings.
- Derive counts, percentages, limits, counter warnings and send eligibility centrally; never infer state from DOM classes or visible text. Keep one canonical draft value behind token rendering and preview so inserted variables survive editing and copy/paste.
- Match the supplied screen layouts using responsive CSS, not fixed-position screenshot coordinates. No rasterized UI text. Use consistent outline icons and the existing Composer mark; do not add an unrelated icon style.
- Verify 1440px and 1024px layouts, fallback glass, keyboard/focus, reduced motion, required Turkish copy, near/over-limit counters, zero audience, all validation states, confirmation and reconnecting. Keep unprovided routes and features out of scope.
