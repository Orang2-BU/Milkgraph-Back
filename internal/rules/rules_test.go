package rules

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

type goldenFixture struct {
	Cases []struct {
		Name        string `json:"name"`
		Limits      Limits `json:"limits"`
		HoldingLeft int    `json:"holdingLeft"`
		Plans       []struct {
			Plan   string `json:"plan"`
			Before struct {
				Checks     map[string]Check `json:"checks"`
				Violations int              `json:"violations"`
				Starvation int              `json:"starvation"`
			} `json:"before"`
			After struct {
				Checks     map[string]Check `json:"checks"`
				Violations int              `json:"violations"`
				Starvation int              `json:"starvation"`
			} `json:"after"`
		} `json:"plans"`
	} `json:"cases"`
	Instructions map[string][]Instruction `json:"instructions"`
}

func fixtures(t *testing.T) (goldenFixture, map[string]Plan) {
	t.Helper()
	var golden goldenFixture
	var scenario struct {
		Plans []Plan `json:"plans"`
	}
	for path, dst := range map[string]any{
		"../../seed/golden-rules.json": &golden,
		"../../seed/scenario.json":     &scenario,
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, dst); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	if len(golden.Cases) != 11 || len(scenario.Plans) != 2 {
		t.Fatalf("fixture counts: golden=%d plans=%d, want 11 and 2", len(golden.Cases), len(scenario.Plans))
	}
	plans := make(map[string]Plan)
	for _, plan := range scenario.Plans {
		plans[plan.ID] = plan
	}
	return golden, plans
}

func TestGoldenEvaluate(t *testing.T) {
	golden, plans := fixtures(t)
	for _, c := range golden.Cases {
		for _, expected := range c.Plans {
			plan, exists := plans[expected.Plan]
			if !exists {
				t.Fatalf("missing plan %s", expected.Plan)
			}
			for view, want := range map[string]map[string]Check{
				"before": expected.Before.Checks,
				"after":  expected.After.Checks,
			} {
				t.Run(c.Name+"/"+expected.Plan+"/"+view, func(t *testing.T) {
					got := Evaluate(plan, view, c.Limits)
					if !reflect.DeepEqual(got, want) {
						for id, check := range want {
							if got[id] != check {
								t.Errorf("%s: got %#v, want %#v", id, got[id], check)
							}
						}
						if len(got) != len(want) {
							t.Errorf("check count = %d, want %d", len(got), len(want))
						}
					}
				})
			}
		}
	}
}

func TestGoldenCheckResult(t *testing.T) {
	golden, plans := fixtures(t)
	if DefaultLimits != golden.Cases[0].Limits {
		t.Fatalf("DefaultLimits = %#v, want %#v", DefaultLimits, golden.Cases[0].Limits)
	}
	for _, c := range golden.Cases {
		if got := HoldingLeft(c.Limits); got != c.HoldingLeft {
			t.Errorf("%s holdingLeft = %d, want %d", c.Name, got, c.HoldingLeft)
		}
		for _, expected := range c.Plans {
			t.Run(c.Name+"/"+expected.Plan, func(t *testing.T) {
				want := CheckResult{
					Before:      expected.Before.Checks,
					After:       expected.After.Checks,
					Violations:  ViewCounts{Before: expected.Before.Violations, After: expected.After.Violations},
					Starvation:  ViewCounts{Before: expected.Before.Starvation, After: expected.After.Starvation},
					HoldingLeft: c.HoldingLeft,
				}
				plan := plans[expected.Plan]
				for name, got := range map[string]CheckResult{
					"CheckPlan":      CheckPlan(plan, c.Limits),
					"EvaluateResult": EvaluateResult(plan, c.Limits),
				} {
					if !reflect.DeepEqual(got, want) {
						t.Errorf("%s = %#v, want %#v", name, got, want)
					}
				}
			})
		}
	}
}

func TestGoldenInstructions(t *testing.T) {
	golden, plans := fixtures(t)
	for id, want := range golden.Instructions {
		t.Run(id, func(t *testing.T) {
			got := Instructions(plans[id])
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("instructions = %#v, want %#v", got, want)
			}
		})
	}
}

func TestChangeoverUsesSOPNotOnlyCount(t *testing.T) {
	_, plans := fixtures(t)
	plan := plans["current"]
	for i := range plan.After {
		if plan.After[i].Type == "changeover" {
			plan.After[i].End = plan.After[i].Start + 20
		}
	}
	got := Evaluate(plan, "after", DefaultLimits)["changeover"]
	want := Check{OK: false, Text: "1 changeover di F1 pukul 16.00, 20 menit; kurang dari SOP 35 menit. Batas 2 changeover per lini per shift."}
	if got != want {
		t.Fatalf("changeover = %#v, want %#v", got, want)
	}
}

func TestInstructionMovesIgnoreDurationOnlyChanges(t *testing.T) {
	plan := Plan{
		ID:     "test",
		Before: []Block{{Resource: "Bay 1", Label: "RT-01", Start: 60, End: 80, Type: "unload"}},
		After: []Block{
			{Resource: "Bay 1", Label: "RT-01", Start: 60, End: 90, Type: "unload"},
			{Resource: "Bay 2", Label: "RT-02", Start: 90, End: 100, Type: "unload"},
		},
	}
	want := []Instruction{{ID: "test:RT-02", Role: "receiving", Text: "RT-02 bongkar di Bay 2, 14.30 - 14.40", From: "Belum terjadwal di rencana awal"}}
	if got := Instructions(plan); !reflect.DeepEqual(got, want) {
		t.Fatalf("instructions = %#v, want %#v", got, want)
	}
	if got := Instructions(Plan{}); got == nil || len(got) != 0 {
		t.Fatalf("empty instructions = %#v, want []", got)
	}
}

func TestBlockJSONKeepsRequiredSource(t *testing.T) {
	data, err := json.Marshal(Block{})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if source, exists := fields["source"]; !exists || source != "" {
		t.Fatalf("block JSON = %s, want required source field", data)
	}
}

func TestJavaScriptFormattingEdges(t *testing.T) {
	for _, c := range []struct {
		offset       int
		clock, hours string
	}{
		{-61, "11.-1", "-2j -1m"},
		{-1, "12.-1", "-1j -1m"},
		{0, "13.00", "0j 00m"},
		{420, "20.00", "7j 00m"},
	} {
		if got := clock(c.offset); got != c.clock {
			t.Errorf("clock(%d) = %q, want %q", c.offset, got, c.clock)
		}
		if got := hours(c.offset); got != c.hours {
			t.Errorf("hours(%d) = %q, want %q", c.offset, got, c.hours)
		}
	}
	for n, want := range map[int]string{0: "0 L/jam", 100: "100 L/jam", 1000: "1.000 L/jam", -1000: "-1.000 L/jam", 1000000: "1.000.000 L/jam"} {
		if got := litres(n); got != want {
			t.Errorf("litres(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestNoCIPMatchesJavaScriptEmptyMaximum(t *testing.T) {
	for _, buffer := range []int{0, 10} {
		limits := DefaultLimits
		limits.CIPBuffer = buffer
		got := Evaluate(Plan{}, "after", limits)["cip"]
		text := "Tidak ada bongkar di Bay 2 sebelum CIP selesai -Infinity.NaN"
		if buffer != 0 {
			text += " (termasuk jeda)"
		}
		want := Check{OK: true, Text: text + "."}
		if got != want {
			t.Fatalf("cip = %#v, want %#v", got, want)
		}
	}
}

func TestPlanUnmarshallingIgnoresExtraFields(t *testing.T) {
	raw := []byte(`{"id":"p1","extra":"ignored","before":[],"after":[]}`)
	var plan Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.ID != "p1" {
		t.Fatalf("plan.ID = %q, want p1", plan.ID)
	}
}

func TestStarvationUsesWorstKnownLineTotal(t *testing.T) {
	blocks := []Block{
		{Resource: "Filling F2", Type: "starvation", Start: 10, End: 20},
		{Resource: "Filling F2", Type: "starvation", Start: 30, End: 50},
		{Resource: "Filling F4", Type: "starvation", Start: 10, End: 35},
		{Resource: "Filling F1", Type: "process", Start: 0, End: 500},
		{Resource: "Filling F9", Type: "starvation", Start: 0, End: 500},
	}
	if got := Starvation(blocks); got != 30 {
		t.Fatalf("Starvation = %d, want 30", got)
	}
	if got := Starvation(nil); got != 0 {
		t.Fatalf("empty Starvation = %d, want 0", got)
	}
}
