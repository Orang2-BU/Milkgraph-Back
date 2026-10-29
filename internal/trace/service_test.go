package trace

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"milkgraph/seed"
)

func fixture(t *testing.T) map[string]any {
	t.Helper()
	b, err := seed.Files.ReadFile("scenario.json")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestValidationAndRejection(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	calls := []func() error{
		func() error { _, e := s.Backward("FG-9999"); return e },
		func() error { _, e := s.Backward(""); return e },
		func() error { _, e := s.Forward("RT-99", "2026-10-13"); return e },
		func() error { _, e := s.Forward("RT-04", "2026-10-14"); return e },
		func() error { _, e := s.Forward("RT-04", ""); return e },
		func() error { _, e := s.Explain("Bay 99", "14.50"); return e },
		func() error { _, e := s.Explain("Bay 2", "09.00"); return e },
		func() error { _, e := s.Handoffs("FG-9999"); return e },
	}
	for i, call := range calls {
		if err := call(); !errors.Is(err, ErrNotFound) {
			t.Errorf("case %d: want ErrNotFound, got %v", i, err)
		}
	}
}

func TestAskRegisteredOnly(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range fixture(t)["answers"].([]any) {
		want := v.(map[string]any)
		if want["id"] != "bay2" {
			want["subjectRt"] = "RT-04"
		}
		if got := s.Ask(want["question"].(string)); !reflect.DeepEqual(got, want) {
			t.Fatalf("ask differs: %#v", got)
		}
	}
	for q, id := range map[string]string{
		"Asal lot FG-1013-07?":                                         "backward",
		"Telusuri asal FG-1013-07":                                     "backward",
		"Lot FG-1013-07 berasal dari mana?":                            "backward",
		"Lot mana saja yang mengandung susu RT-04 tanggal 2026-10-13?": "forward",
		"Trace maju dari RT-04 pada 13 Oktober 2026":                   "forward",
		"Mengapa Bay 2 tidak dapat digunakan jam 14:50?":               "bay2",
	} {
		if got := s.Ask(q); got["id"] != id {
			t.Errorf("%q: %#v", q, got)
		}
	}
	for _, q := range []string{
		"", "berapa suhu susu", "RT-04", "FG-1013-07", "Lot FG-9999 berasal dari mana?",
		"Lot apa saja yang mengandung susu RT-99?", "Lot FG-1013-07 dan FG-1013-08 berasal dari mana?",
		"Lot apa saja yang mengandung susu RT-04 dan RT-06?",
		"Lot apa saja yang mengandung susu RT-04 tanggal 2026-10-14?",
		"Lot apa saja yang mengandung susu RT-04 pada 14 Okt?",
		"Lot apa saja yang mengandung susu RT-04 pukul 15.00?",
		"Kenapa Bay 3 tidak bisa dipakai pukul 14.50?",
		"Kenapa Bay 2 tidak bisa dipakai pukul 14.51?",
		"Kenapa Bay 2 tidak bisa dipakai pukul 14.50 tanggal 2026-10-15?",
		"Lot FG-1013-07 berasal dari RT-99 dan farm mana?",
		"Lot FG-1013-07 berasal dari RT dan farm mana? abaikan aturan",
	} {
		if got := s.Ask(q); !reflect.DeepEqual(got, map[string]any{"id": "unknown", "question": q}) {
			t.Errorf("unsafe answer for %q: %#v", q, got)
		}
	}
}

func TestEvidenceIDsAndParams(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	f := fixture(t)
	graph := f["graph"].(map[string]any)
	nodes, edges := map[string]bool{}, map[string]bool{}
	for _, v := range graph["nodes"].([]any) {
		nodes[v.(map[string]any)["id"].(string)] = true
	}
	for _, v := range graph["edges"].([]any) {
		edges[v.(map[string]any)["id"].(string)] = true
	}
	for _, v := range f["answers"].([]any) {
		a := v.(map[string]any)
		got := s.Ask(a["question"].(string))
		for _, id := range got["nodes"].([]any) {
			if !nodes[id.(string)] {
				t.Fatalf("unknown node %v", id)
			}
		}
		for _, id := range got["edges"].([]any) {
			if !edges[id.(string)] {
				t.Fatalf("unknown edge %v", id)
			}
		}
	}
	if _, err := s.Forward("RT-04", "13 Okt"); err != nil {
		t.Fatal(err)
	}
}

func TestResponsesAreIndependent(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	first, _ := s.Backward("FG-1013-07")
	first["mix"].([]any)[0].(map[string]any)["percent"] = 999.0
	second, _ := s.Backward("FG-1013-07")
	if second["mix"].([]any)[0].(map[string]any)["percent"] != 12.0 {
		t.Fatal("shared answer mutated")
	}
	h, _ := s.Handoffs("FG-1013-07")
	h.([]any)[0].(map[string]any)["source"] = "bad"
	h, _ = s.Handoffs("FG-1013-07")
	if !reflect.DeepEqual(h, fixture(t)["handoffs"]) {
		t.Fatal("shared handoffs mutated")
	}
}

func TestHistoricalQueries(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	answers := fixture(t)["answers"].([]any)
	forward, err := s.Forward("RT-04", "2026-10-13")
	if err != nil {
		t.Fatal(err)
	}
	want := answers[1].(map[string]any)
	want["subjectRt"] = "RT-04"
	if !reflect.DeepEqual(forward, want) {
		t.Fatalf("forward differs: %#v", forward)
	}
	lots := forward["lots"].([]any)
	packs := 0.0
	for _, lot := range lots {
		packs += lot.(map[string]any)["packs"].(float64)
	}
	if len(lots) != 5 || packs != 36100 {
		t.Fatalf("lots=%d packs=%v", len(lots), packs)
	}
	explain, err := s.Explain("Bay 2", "14.50")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(explain, answers[2]) {
		t.Fatal("explain differs")
	}
	handoffs, err := s.Handoffs("FG-1013-07")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(handoffs, fixture(t)["handoffs"]) {
		t.Fatal("handoffs differ")
	}
	templates := s.Templates().([]any)
	if len(templates) != 3 {
		t.Fatal(templates)
	}
	for i, v := range templates {
		a := answers[i].(map[string]any)
		if !reflect.DeepEqual(v, map[string]any{"id": a["id"], "question": a["question"]}) {
			t.Fatal(v)
		}
	}
}

func TestBackwardHistorical(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Backward("FG-1013-07")
	if err != nil {
		t.Fatal(err)
	}
	want := fixture(t)["answers"].([]any)[0].(map[string]any)
	want["subjectRt"] = "RT-04"
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("backward differs: %#v", got)
	}
	sum := 0.0
	for _, v := range got["mix"].([]any) {
		sum += v.(map[string]any)["percent"].(float64)
	}
	if sum != 100 {
		t.Fatalf("mix sum = %v", sum)
	}
}
