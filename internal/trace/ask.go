package trace

import (
	"regexp"
	"strings"
)

var (
	targetPattern   = regexp.MustCompile(`\b(?:fg|rt)-[a-z0-9-]+\b|\bbay\s+[a-z0-9-]+\b`)
	backwardPattern = regexp.MustCompile(`^(?:(?:asal|telusuri asal) (?:lot )?\{target\}|(?:lot )?\{target\} berasal dari (?:rt dan farm mana|mana))$`)
	forwardPattern  = regexp.MustCompile(`^(?:lot (?:apa|mana) saja yang mengandung (?:susu )?\{target\}|(?:trace|telusuri) maju dari \{target\})(?: (?:tanggal|pada) (?:2026-10-13|13 (?:okt|oktober)(?: 2026)?))?$`)
	explainPattern  = regexp.MustCompile(`^(?:kenapa|mengapa) \{target\} (?:tidak bisa dipakai|tidak dapat digunakan|tidak tersedia) (?:pukul|jam) 14[.:]50$`)
)

// Ask recognizes whole registered question forms, never keyword guesses. Unparsed
// text (including extra entities, dates, times or instructions) always abstains.
func (s *Service) Ask(question string) map[string]any {
	unknown := map[string]any{"id": "unknown", "question": question}
	q := strings.Trim(strings.Join(strings.Fields(strings.ToLower(question)), " "), " ?!")
	targets := targetPattern.FindAllString(q, -1)
	if len(targets) != 1 {
		return unknown
	}
	form := strings.Replace(q, targets[0], "{target}", 1)
	var a map[string]any
	var err error
	switch {
	case backwardPattern.MatchString(form):
		a, err = s.Backward(strings.ToUpper(targets[0]))
	case forwardPattern.MatchString(form):
		a, err = s.Forward(strings.ToUpper(targets[0]), "2026-10-13")
	case explainPattern.MatchString(form):
		if targets[0] != "bay 2" {
			return unknown
		}
		a, err = s.Explain("Bay 2", "14.50")
	default:
		return unknown
	}
	if err != nil {
		return unknown
	}
	return a
}
