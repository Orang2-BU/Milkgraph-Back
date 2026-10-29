package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"milkgraph/internal/rules"
	"milkgraph/seed"
)

type limitGroup struct {
	Params []limitParam `json:"params"`
}

type limitParam struct {
	Key   string `json:"key"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Unit  string `json:"unit"`
	Min   int    `json:"min"`
	Max   int    `json:"max"`
	Step  int    `json:"step"`
}

var limitSchemaJSON = func() json.RawMessage {
	raw, err := seed.Files.ReadFile("limit-schema.json")
	if err != nil {
		panic(err)
	}
	return json.RawMessage(raw)
}()

var limitGroups = func() []limitGroup {
	var groups []limitGroup
	if err := json.Unmarshal(limitSchemaJSON, &groups); err != nil {
		panic(err)
	}
	return groups
}()

func (s *Server) limitSchema() any { return limitSchemaJSON }

func limitNumbers(l rules.Limits) map[string]int {
	return map[string]int{
		"maxTransfer": l.MaxTransfer, "starvationTol": l.StarvationTol,
		"holdingMin": l.HoldingMin, "bufferMaxMin": l.BufferMaxMin,
		"cipBuffer": l.CIPBuffer, "tankMax": l.TankMax, "changeoverMax": l.ChangeoverMax,
	}
}

func validateLimits(limits rules.Limits) error {
	values := limitNumbers(limits)
	for _, group := range limitGroups {
		for _, p := range group.Params {
			if p.Kind != "range" {
				continue
			}
			v := values[p.Key]
			if v < p.Min || v > p.Max || (v-p.Min)%p.Step != 0 {
				return invalid(fmt.Sprintf("%s harus antara %d dan %d (kelipatan %d).", p.Label, p.Min, p.Max, p.Step), p.Key)
			}
		}
	}
	return nil
}
func formatThousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s
}

func formatDurationHM(minutes int) string {
	return fmt.Sprintf("%dj %02dm", minutes/60, minutes%60)
}

func formatLimitsDiff(old, next rules.Limits) string {
	before, after := limitNumbers(old), limitNumbers(next)
	oldToggles := map[string]bool{"maintenanceBay3": old.MaintenanceBay3, "breakdownF1": old.BreakdownF1}
	newToggles := map[string]bool{"maintenanceBay3": next.MaintenanceBay3, "breakdownF1": next.BreakdownF1}
	var diffs []string
	for _, group := range limitGroups {
		for _, p := range group.Params {
			if p.Kind == "toggle" {
				if oldToggles[p.Key] == newToggles[p.Key] {
					continue
				}
				state := "dimatikan"
				if newToggles[p.Key] {
					state = "diaktifkan"
				}
				diffs = append(diffs, p.Label+" "+state)
				continue
			}
			if before[p.Key] == after[p.Key] {
				continue
			}
			show := func(v int) string {
				if p.Key == "holdingMin" || p.Key == "bufferMaxMin" {
					return formatDurationHM(v)
				}
				return formatThousands(v) + " " + p.Unit
			}
			diffs = append(diffs, fmt.Sprintf("%s %s → %s", p.Label, show(before[p.Key]), show(after[p.Key])))
		}
	}
	return strings.Join(diffs, "; ")
}
