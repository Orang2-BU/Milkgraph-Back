package trace

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func TestPropagateRejectsInvalidInput(t *testing.T) {
	at := time.Now()
	cases := []struct {
		name      string
		sources   []string
		initial   map[string]Vector
		transfers []Transfer
	}{
		{"unknown source", []string{"RT-1"}, map[string]Vector{"tank": {"RT-99": 1}}, nil},
		{"negative source", []string{"RT-1"}, map[string]Vector{"tank": {"RT-1": -1}}, nil},
		{"nan source", []string{"RT-1"}, map[string]Vector{"tank": {"RT-1": math.NaN()}}, nil},
		{"infinite source", []string{"RT-1"}, map[string]Vector{"tank": {"RT-1": math.Inf(1)}}, nil},
	}
	for _, volume := range []float64{-1, math.NaN(), math.Inf(1), 11} {
		cases = append(cases, struct {
			name      string
			sources   []string
			initial   map[string]Vector
			transfers []Transfer
		}{"invalid transfer", []string{"RT-1"}, map[string]Vector{"a": {"RT-1": 10}, "b": {}}, []Transfer{{At: at, From: "a", To: "b", Volume: volume}}})
	}
	for _, tr := range []Transfer{
		{At: at, From: "unknown", To: "b", Volume: 1}, {At: at, From: "a", To: "unknown", Volume: 1},
		{From: "a", To: "b", Volume: 1}, {At: at, From: "a", To: "a", Volume: 1},
	} {
		cases = append(cases, struct {
			name      string
			sources   []string
			initial   map[string]Vector
			transfers []Transfer
		}{"invalid endpoint/time", []string{"RT-1"}, map[string]Vector{"a": {"RT-1": 10}, "b": {}}, []Transfer{tr}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Propagate(tc.sources, tc.initial, tc.transfers)
			if err == nil || got != nil {
				t.Fatalf("wanted error/nil, got %v/%v", got, err)
			}
		})
	}
}

func TestPropagateStrictCapacityAndOverflow(t *testing.T) {
	at := time.Now()
	for _, tc := range []struct {
		initial map[string]Vector
		volume  float64
	}{
		{map[string]Vector{"a": {"RT-1": 1}, "b": {}}, math.Nextafter(1, 2)},
		{map[string]Vector{"a": {"RT-1": math.MaxFloat64, "RT-2": math.MaxFloat64}, "b": {}}, 1},
		{map[string]Vector{"a": {"RT-1": math.MaxFloat64}, "b": {"RT-1": math.MaxFloat64}}, math.MaxFloat64},
	} {
		if got, err := Propagate([]string{"RT-1", "RT-2"}, tc.initial, []Transfer{{At: at, From: "a", To: "b", Volume: tc.volume}}); err == nil || got != nil {
			t.Fatalf("accepted overflow: %v %v", got, err)
		}
	}
}

func TestPropagateMixedPartialTransfer(t *testing.T) {
	at := time.Now()
	got, err := Propagate([]string{"RT-1", "RT-2"}, map[string]Vector{"tank": {"RT-1": 30, "RT-2": 70}, "lot": {}}, []Transfer{{At: at, From: "tank", To: "lot", Volume: 20}})
	if err != nil {
		t.Fatal(err)
	}
	if got["lot"]["RT-1"] != 6 || got["lot"]["RT-2"] != 14 || got["tank"]["RT-1"] != 24 || got["tank"]["RT-2"] != 56 {
		t.Fatal(got)
	}
}

func TestPropagatePerfectMixtureInTimeOrder(t *testing.T) {
	initial := map[string]Vector{
		"RT-1": {"RT-1": 100}, "RT-2": {"RT-2": 100},
		"tank": {}, "lot-1": {}, "lot-2": {},
	}
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	transfers := []Transfer{
		{At: at.Add(4 * time.Hour), From: "tank", To: "lot-2", Volume: 150},
		{At: at, From: "RT-1", To: "tank", Volume: 100},
		{At: at.Add(time.Hour), From: "tank", To: "lot-1", Volume: 50},
		{At: at.Add(2 * time.Hour), From: "RT-2", To: "tank", Volume: 100},
	}
	got, err := Propagate([]string{"RT-1", "RT-2"}, initial, transfers)
	if err != nil {
		t.Fatal(err)
	}
	if got["lot-1"]["RT-1"] != 50 || got["lot-1"]["RT-2"] != 0 {
		t.Fatal(got)
	}
	if got["lot-2"]["RT-1"] != 50 || got["lot-2"]["RT-2"] != 100 {
		t.Fatal(got)
	}
	for _, source := range []string{"RT-1", "RT-2"} {
		total := 0.0
		for _, v := range got {
			total += v[source]
		}
		if math.Abs(total-100) > 1e-10 {
			t.Fatalf("%s volume = %v", source, total)
		}
	}
	if !reflect.DeepEqual(initial["tank"], Vector{}) || initial["RT-1"]["RT-1"] != 100 {
		t.Fatal("input mutated")
	}
	if transfers[0].To != "lot-2" {
		t.Fatal("transfer order mutated")
	}
}
