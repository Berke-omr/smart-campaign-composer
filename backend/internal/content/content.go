// Package content holds the rules for campaign messages: channels and their
// length limits, personalization variables, validation and rendering
// (DECISIONS.md D15). The same rules apply to Claude's drafts and to the
// marketer's edits.
package content

// Channel is a delivery channel.
type Channel string

const (
	SMS   Channel = "sms"
	Email Channel = "email"
)

// Channels lists every channel in a stable order.
var Channels = []Channel{SMS, Email}

// Valid reports whether c is a known channel.
func (c Channel) Valid() bool {
	return c == SMS || c == Email
}

// Limits from the case, counted in Unicode characters (runes), not bytes.
const (
	MaxSMSRunes     = 160
	MaxSubjectRunes = 80
)

// Variable is a personalization variable, written {{name}} in a template.
type Variable string

const (
	FirstName Variable = "first_name"
	LastName  Variable = "last_name"
	City      Variable = "city"
)

// Variables is the whitelist, in a stable order.
var Variables = []Variable{FirstName, LastName, City}

// Placeholder is the variable as written in a template, e.g. "{{first_name}}".
func (v Variable) Placeholder() string {
	return "{{" + string(v) + "}}"
}

// Values maps each variable to one value: a recipient's data, the fictional
// sample user, or the longest values in an audience.
type Values map[Variable]string

// Message is one channel's content. SMS uses Text; email uses Subject and Body.
type Message struct {
	Text    string `json:"text,omitempty"`
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body,omitempty"`
}
