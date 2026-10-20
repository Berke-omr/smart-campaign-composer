package llm

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/content"
	"github.com/Berke-omr/smart-campaign-composer/backend/internal/segment"
)

// Prompts are split into a stable system prompt (identical for every request
// of a task, so it is served from the prompt cache) and a user message with
// the request's data. The brief is wrapped in tags and treated as data: it
// says what the marketer wants, but it cannot change the rules.

// Tone is the voice the marketer picks for the messages.
type Tone string

const (
	ToneFriendly     Tone = "friendly"
	ToneProfessional Tone = "professional"
	TonePlayful      Tone = "playful"
	ToneUrgent       Tone = "urgent"
)

// Tones lists the accepted tones; the first is the default.
var Tones = []Tone{ToneFriendly, ToneProfessional, TonePlayful, ToneUrgent}

// Valid reports whether t is a known tone.
func (t Tone) Valid() bool {
	for _, x := range Tones {
		if t == x {
			return true
		}
	}
	return false
}

// Repair carries a rejected answer and what was wrong with it, for the next
// attempt (DECISIONS.md D10). Each attempt is a fresh single-turn request.
type Repair struct {
	Previous string
	Problems []string
}

const segmentSystemTemplate = `You turn a marketer's campaign brief into an audience rule for an online store's customer base. You never write SQL: you fill in the JSON structure you are given, and the system validates the rule before it counts or messages anyone.

Fields you can target (nothing else):
%s
How to map common requests:
- "added to cart in the last N days" -> last_cart_add_at within_last_days N.
- "did not buy in the last N days" -> last_purchase_at not_within_last_days N. This already includes people who never bought anything.
- "never bought" -> last_purchase_at is_null.
- Several cities -> one city condition with "in".
- A brief that names no audience criteria ("all subscribers") -> match "all" with no conditions.
- Use match "all" unless the brief clearly asks for one criterion or another.

The system applies channel consent itself. Never add conditions about opt-in, consent, email addresses or phone numbers.

If the brief asks for something no field can express (for example gender, age, product category, device or browsing behaviour), do not approximate it with another field. Leave it out of the rule and list it in unsupported_criteria as a short phrase in the brief's language. Otherwise unsupported_criteria is empty.

The brief may also describe the message itself (discount, tone, length). That part is for the copywriter; only the audience goes into the rule.

campaign_name is a short English title of two to five words for the campaign list, such as "Cart reminder — İstanbul" or "Win-back 90 days". When the audience is a single city, end the name with an em dash and the city.`

// SegmentSystem is the system prompt for TaskSegment. It lists the
// registry's fields, so the model's instructions and the validator come from
// the same source.
func SegmentSystem(reg *segment.Registry) string {
	var b strings.Builder
	for _, f := range reg.Fields() {
		fmt.Fprintf(&b, "- %s: %s Operators: %s.", f.Name, f.Description, joinOps(f.Ops))
		switch f.Type {
		case segment.TypeEnum:
			if len(f.Enum) <= 5 {
				fmt.Fprintf(&b, " Values: %s.", strings.Join(f.Enum, ", "))
			} else {
				b.WriteString(" Values: the exact spellings listed in the schema.")
			}
		case segment.TypeRelativeTime:
			fmt.Fprintf(&b, " Days: %d to %d.", segment.MinDays, segment.MaxDays)
		case segment.TypeInteger, segment.TypeNumber:
			fmt.Fprintf(&b, " Range: %s to %s.", num(f.Min), num(f.Max))
		}
		b.WriteString("\n")
	}
	return fmt.Sprintf(segmentSystemTemplate, b.String())
}

// SegmentPrompt is the user message for TaskSegment.
func SegmentPrompt(brief string, repair *Repair) string {
	var b strings.Builder
	b.WriteString("Build the audience rule for this brief.\n\n<brief>\n")
	b.WriteString(strings.TrimSpace(brief))
	b.WriteString("\n</brief>\n")
	writeRepair(&b, repair)
	return b.String()
}

const contentSystemBase = `You write marketing messages for an online store's customers. A marketer gives you a brief, a tone and the audience; a person reviews and approves every message before anything is sent.

Rules for every message:
- Write in the language of the brief, with correct characters (for Turkish: ç, ğ, ı, İ, ö, ş, ü).
- Personalize only with these variables, written exactly like this: {{first_name}}, {{last_name}}, {{city}}. No other placeholders.
- Never attach a suffix to a variable (not "{{city}}'daki", "{{first_name}}'cığım"): the value differs per person, and a Turkish suffix that fits one value is wrong for another. Rephrase instead, for example "{{city}} için".
- State only what the brief says. Do not invent discount codes, prices, deadlines, product names or links. If the brief gives a link, use it as given.
- Plain words only: no emoji or other pictographic symbols (such as 😊, ✨, ⭐, ✔) in any field, whatever the tone.
- Follow the tone. Wording about tone in the brief itself (for example "samimi ama kısa") takes precedence.
- Lengths are counted after the variables are filled in. The request tells you how many characters each variable takes in this audience.
`

const smsSystem = contentSystemBase + `
You are writing the SMS:
- text: one plain-text message without line breaks, at most 160 characters including the filled-in variables. Shorter is better.`

const emailSystem = contentSystemBase + `
You are writing the email:
- subject: one line, at most 80 characters including the filled-in variables.
- body: plain text, no HTML or Markdown: a greeting, two to four short paragraphs and a short sign-off. Separate paragraphs with a blank line.`

// ContentSystem is the system prompt for one channel's draft.
func ContentSystem(ch content.Channel) string {
	if ch == content.Email {
		return emailSystem
	}
	return smsSystem
}

// ContentBrief is what a copywriter needs to know about the campaign.
type ContentBrief struct {
	Brief        string
	Tone         Tone
	CampaignName string
	// Audience is the human-readable rule (segment.Describe lines).
	Audience []string
	// Longest holds, for each variable, the longest value among the people
	// this channel will reach (DECISIONS.md D15).
	Longest content.Values
}

// ContentPrompt is the user message for one channel's draft.
func ContentPrompt(ch content.Channel, cb ContentBrief, repair *Repair) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Write the %s for this campaign.\n\n<brief>\n%s\n</brief>\n\n", channelName(ch), strings.TrimSpace(cb.Brief))
	fmt.Fprintf(&b, "Tone: %s\n", cb.Tone)
	fmt.Fprintf(&b, "Campaign: %s\n", cb.CampaignName)
	if len(cb.Audience) == 0 {
		b.WriteString("Audience: all users\n")
	} else {
		fmt.Fprintf(&b, "Audience: %s\n", strings.Join(cb.Audience, "; "))
	}
	b.WriteString("\nCharacters each variable takes (the longest value among this audience):\n")
	for _, v := range content.Variables {
		val := cb.Longest[v]
		fmt.Fprintf(&b, "- %s: %d (%q)\n", v.Placeholder(), utf8.RuneCountInString(val), val)
	}
	writeRepair(&b, repair)
	return b.String()
}

func writeRepair(b *strings.Builder, r *Repair) {
	if r == nil {
		return
	}
	b.WriteString("\nYour previous answer was rejected:\n<previous_answer>\n")
	b.WriteString(r.Previous)
	b.WriteString("\n</previous_answer>\n\nProblems found by the validator:\n")
	for _, p := range r.Problems {
		fmt.Fprintf(b, "- %s\n", p)
	}
	b.WriteString("\nReturn a corrected answer that fixes every problem.\n")
}

func channelName(ch content.Channel) string {
	if ch == content.Email {
		return "email"
	}
	return "SMS"
}

func num(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }

func joinOps(ops []segment.Op) string {
	s := make([]string, len(ops))
	for i, o := range ops {
		s[i] = string(o)
	}
	return strings.Join(s, ", ")
}
