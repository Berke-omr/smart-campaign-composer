package mock

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/Berke-omr/smart-campaign-composer/backend/internal/llm"
)

// The fallback answers a brief that matches no scenario with a small
// keyword reading of it. It is deliberately simple: it lets a reviewer
// without a key try their own wording end to end, and every answer still
// goes through the same validators as Claude's.

var (
	daysPattern     = regexp.MustCompile(`(\d{1,3})\s*(gün|gun|day)`)
	discountPattern = regexp.MustCompile(`%\s?(\d{1,2})|(\d{1,2})\s?%`)
	cartWords       = []string{"sepet", "cart"}
	noPurchaseWords = []string{
		"alışveriş yapmayan", "alışveriş yapmamış", "satın almayan", "satın almamış", "almayan", "almamış",
		"no purchase", "not purchased", "haven't bought", "didn't buy", "did not buy",
	}
	turkishMarks = "çğıöşüÇĞİÖŞÜ"
)

func fallback(req llm.Request, cities []string) Attempt {
	brief := lowerTR(req.Brief)
	turkish := strings.ContainsAny(req.Brief, turkishMarks)
	discount := ""
	if m := discountPattern.FindStringSubmatch(req.Brief); m != nil {
		discount = "%" + m[1] + m[2]
	}

	var answer any
	switch req.Task {
	case llm.TaskSegment:
		answer = fallbackSegment(brief, cities)
	case llm.TaskSMS:
		answer = map[string]string{"text": fallbackSMS(turkish, discount)}
	case llm.TaskEmail:
		subject, body := fallbackEmail(turkish, discount)
		answer = map[string]string{"subject": subject, "body": body}
	}
	raw, _ := json.Marshal(answer)
	return Attempt{Answer: raw}
}

func fallbackSegment(brief string, cities []string) map[string]any {
	days := 30
	if m := daysPattern.FindStringSubmatch(brief); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 1 && n <= 730 {
			days = n
		}
	}
	var conds []map[string]any
	var found []string
	for _, c := range cities {
		if strings.Contains(brief, lowerTR(c)) {
			found = append(found, c)
		}
	}
	switch len(found) {
	case 0:
	case 1:
		conds = append(conds, map[string]any{"field": "city", "op": "eq", "value": found[0]})
	default:
		conds = append(conds, map[string]any{"field": "city", "op": "in", "value": found})
	}
	if containsAny(brief, cartWords) {
		conds = append(conds, map[string]any{"field": "last_cart_add_at", "op": "within_last_days", "value": days})
	}
	if containsAny(brief, noPurchaseWords) {
		conds = append(conds, map[string]any{"field": "last_purchase_at", "op": "not_within_last_days", "value": days})
	}
	if conds == nil {
		conds = []map[string]any{}
	}
	name := "Custom campaign"
	if len(found) == 1 {
		name = "Custom campaign — " + found[0]
	}
	return map[string]any{
		"campaign_name":        name,
		"rule":                 map[string]any{"match": "all", "conditions": conds},
		"unsupported_criteria": []string{},
	}
}

func fallbackSMS(turkish bool, discount string) string {
	switch {
	case turkish && discount != "":
		return "Merhaba {{first_name}}, sana özel " + discount + " indirim seni bekliyor! Fırsatı kaçırma."
	case turkish:
		return "Merhaba {{first_name}}, sana özel fırsatlar seni bekliyor! Hemen göz at."
	case discount != "":
		return "Hi {{first_name}}, a " + discount + " discount is waiting for you. Don't miss it!"
	default:
		return "Hi {{first_name}}, something special is waiting for you. Take a look!"
	}
}

func fallbackEmail(turkish bool, discount string) (subject, body string) {
	if turkish {
		subject = "{{first_name}}, sana özel bir fırsat var"
		offer := "Sana özel fırsatlar seni bekliyor."
		if discount != "" {
			subject = "{{first_name}}, sana özel " + discount + " indirim"
			offer = "Sana özel " + discount + " indirim seni bekliyor."
		}
		return subject, "Merhaba {{first_name}},\n\n" + offer + "\n\nDetaylar için uygulamaya göz atabilirsin.\n\nMağaza Ekibi"
	}
	subject = "{{first_name}}, something special for you"
	offer := "Something special is waiting for you."
	if discount != "" {
		subject = "{{first_name}}, " + discount + " off, just for you"
		offer = "A " + discount + " discount is waiting for you."
	}
	return subject, "Hi {{first_name}},\n\n" + offer + "\n\nTake a look in the app for the details.\n\nThe Store Team"
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}
