package graph

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"milkgraph/seed"
)

func scenario(t *testing.T) map[string]any {
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

func TestViewMatchesFrontendAndIsIndependent(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../../Milkgraph-Front/mock/scenario.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.View(), fixture["graph"]) {
		t.Fatal("graph differs from frontend fixture")
	}
	v := s.View()
	v["nodes"].([]any)[0].(map[string]any)["label"] = "changed"
	if !reflect.DeepEqual(s.View(), fixture["graph"]) {
		t.Fatal("caller mutated stored graph")
	}
}

func TestProjectRejectsInvalidGraph(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"unknown provenance", func(v map[string]any) { v["nodes"].([]any)[0].(map[string]any)["source"] = "missing-source" }},
		{"endpoint is provenance not node", func(v map[string]any) { v["edges"].([]any)[0].(map[string]any)["source"] = "TR-RMT2" }},
		{"duplicate node", func(v map[string]any) { a := v["nodes"].([]any); v["nodes"] = append(a, a[0]) }},
		{"duplicate edge", func(v map[string]any) { a := v["edges"].([]any); v["edges"] = append(a, a[0]) }},
		{"bad rank", func(v map[string]any) { v["nodes"].([]any)[0].(map[string]any)["rank"] = -1 }},
		{"bad planned", func(v map[string]any) { v["edges"].([]any)[0].(map[string]any)["planned"] = "yes" }},
		{"bad collection", func(v map[string]any) { v["nodes"] = "not an array" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := s.View()
			tc.change(v)
			if _, err := s.Project(v); err == nil {
				t.Fatal("accepted invalid graph")
			}
		})
	}
	if _, err := s.Subgraph("rt-04", 33); err == nil {
		t.Fatal("unbounded depth accepted")
	}
}

func TestGraphIntegrityAnd16NodeTypes(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	view := s.View()
	nodes, ok := view["nodes"].([]any)
	if !ok || len(nodes) != 30 {
		t.Fatalf("expected 30 nodes, got %d", len(nodes))
	}
	edges, ok := view["edges"].([]any)
	if !ok || len(edges) != 28 {
		t.Fatalf("expected 28 edges, got %d", len(edges))
	}

	types := make(map[string]int)
	nodeIDs := make(map[string]bool)
	for _, n := range nodes {
		nm := n.(map[string]any)
		id := nm["id"].(string)
		if nodeIDs[id] {
			t.Fatalf("duplicate node id: %s", id)
		}
		nodeIDs[id] = true
		tp := nm["type"].(string)
		types[tp]++
		src := nm["source"].(string)
		if src == "" {
			t.Fatalf("node %s missing provenance source", id)
		}
	}
	if len(types) != 16 {
		t.Fatalf("expected 16 node types, got %d: %v", len(types), types)
	}

	edgeIDs := make(map[string]bool)
	for _, e := range edges {
		em := e.(map[string]any)
		id := em["id"].(string)
		if edgeIDs[id] {
			t.Fatalf("duplicate edge id: %s", id)
		}
		edgeIDs[id] = true
		src := em["source"].(string)
		tgt := em["target"].(string)
		if !nodeIDs[src] {
			t.Fatalf("edge %s has non-existent source node: %s", id, src)
		}
		if !nodeIDs[tgt] {
			t.Fatalf("edge %s has non-existent target node: %s", id, tgt)
		}
	}
}

func TestSubgraphDirectedTraversal(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}

	// 1. ErrNotFound on unknown node
	_, err = s.Subgraph("non-existent", 1)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// 2. Depth 0 returns only the root node and 0 edges
	sg0, err := s.Subgraph("rt-04", 0)
	if err != nil {
		t.Fatal(err)
	}
	nodes0 := sg0["nodes"].([]any)
	edges0 := sg0["edges"].([]any)
	if len(nodes0) != 1 || nodes0[0].(map[string]any)["id"] != "rt-04" {
		t.Fatalf("expected 1 root node rt-04, got %v", nodes0)
	}
	if len(edges0) != 0 {
		t.Fatalf("expected 0 edges at depth 0, got %d", len(edges0))
	}

	// 3. Depth 1 directed traversal from rt-04
	// rt-04 -> rmt-2 (edge e-rt04)
	sg1, err := s.Subgraph("rt-04", 1)
	if err != nil {
		t.Fatal(err)
	}
	nodes1 := sg1["nodes"].([]any)
	edges1 := sg1["edges"].([]any)
	if len(nodes1) != 2 {
		t.Fatalf("expected 2 nodes (rt-04, rmt-2), got %d", len(nodes1))
	}
	if len(edges1) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges1))
	}
	// Deterministic order: check that calling Subgraph multiple times produces identical slice ordering
	for i := 0; i < 5; i++ {
		repeat, err := s.Subgraph("rt-04", 1)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(sg1, repeat) {
			t.Fatal("subgraph order is non-deterministic")
		}
	}

	// 4. Multi-hop traversal from farm-b (farm-b -> rt-04 -> rmt-2 -> th-1 -> sp-1 -> ...)
	sg3, err := s.Subgraph("farm-b", 3)
	if err != nil {
		t.Fatal(err)
	}
	nodes3 := sg3["nodes"].([]any)
	if len(nodes3) != 4 { // farm-b, rt-04, rmt-2, th-1
		t.Fatalf("expected 4 nodes at depth 3 from farm-b, got %d", len(nodes3))
	}

	// 5. Negative depth rejected
	_, err = s.Subgraph("farm-b", -1)
	if err == nil {
		t.Fatal("expected error for negative depth")
	}
}

func TestProjectDynamicUpdates(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}

	// Project adding a new Lot and edge from f2
	newLot := map[string]any{
		"id":     "fg-99",
		"type":   "Lot",
		"label":  "FG-1014-99",
		"rank":   float64(10),
		"source": "TR-FILL",
	}
	newEdge := map[string]any{
		"id":      "e-f2-fg99",
		"source":  "f2",
		"target":  "fg-99",
		"label":   "FILLED 10%",
		"planned": true,
	}

	projected, err := s.Project(map[string]any{
		"nodes": []any{newLot},
		"edges": []any{newEdge},
	})
	if err != nil {
		t.Fatalf("failed to project: %v", err)
	}

	// Base service must remain unchanged (30 nodes, 28 edges)
	if len(s.View()["nodes"].([]any)) != 30 {
		t.Fatal("base service modified after project")
	}

	// Projected service has 31 nodes and 29 edges
	pView := projected.View()
	if len(pView["nodes"].([]any)) != 31 {
		t.Fatalf("expected 31 nodes in projected graph, got %d", len(pView["nodes"].([]any)))
	}
	if len(pView["edges"].([]any)) != 29 {
		t.Fatalf("expected 29 edges in projected graph, got %d", len(pView["edges"].([]any)))
	}

	// Rejection on invalid node type
	_, err = s.Project(map[string]any{
		"nodes": []any{map[string]any{"id": "bad-1", "type": "InvalidType", "label": "Bad", "rank": float64(1), "source": "TR-FILL"}},
	})
	if err == nil {
		t.Fatal("expected rejection of invalid node type")
	}

	// Rejection on broken edge endpoint
	_, err = s.Project(map[string]any{
		"edges": []any{map[string]any{"id": "bad-e", "source": "f2", "target": "non-existent", "label": "X"}},
	})
	if err == nil {
		t.Fatal("expected rejection of broken edge endpoint")
	}
}

func TestTraceAskAndAnswersIDsReferenceCheck(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	sc := scenario(t)
	answers := sc["answers"].([]any)
	view := s.View()
	nodeSet := make(map[string]bool)
	for _, n := range view["nodes"].([]any) {
		nodeSet[n.(map[string]any)["id"].(string)] = true
	}
	edgeSet := make(map[string]bool)
	for _, e := range view["edges"].([]any) {
		edgeSet[e.(map[string]any)["id"].(string)] = true
	}

	for _, aRaw := range answers {
		a := aRaw.(map[string]any)
		id := a["id"].(string)
		for _, nid := range a["nodes"].([]any) {
			if !nodeSet[nid.(string)] {
				t.Errorf("answer %s references unknown graph node: %s", id, nid)
			}
		}
		for _, eid := range a["edges"].([]any) {
			if !edgeSet[eid.(string)] {
				t.Errorf("answer %s references unknown graph edge: %s", id, eid)
			}
		}
	}
}

func TestTraversalRegressionAndCycleHandling(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	// Add a cycle in projection: rt-04 -> rmt-2 -> cycle back to rt-04
	cycleEdge := map[string]any{
		"id":     "e-cycle-test",
		"source": "rmt-2",
		"target": "rt-04",
		"label":  "RECYCLE",
	}
	projected, err := s.Project(map[string]any{
		"edges": []any{cycleEdge},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Subgraph traversal with depth 10 must not infinite loop or panic
	res, err := projected.Subgraph("rt-04", 10)
	if err != nil {
		t.Fatalf("cycle traversal failed: %v", err)
	}
	nodes := res["nodes"].([]any)
	if len(nodes) == 0 {
		t.Fatal("expected nodes in traversal with cycle")
	}
}

func TestGraphTemporalAndContextRelations(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	view := s.View()
	edges := view["edges"].([]any)
	edgeMap := make(map[string]map[string]any)
	for _, e := range edges {
		em := e.(map[string]any)
		edgeMap[em["id"].(string)] = em
	}

	// 1. Planned edge (future event, not completed yet)
	eRt26Bay2, ok := edgeMap["e-rt26-bay2"]
	if !ok || eRt26Bay2["planned"] != true || eRt26Bay2["label"] != "PLANNED 14.50" {
		t.Fatalf("expected planned edge e-rt26-bay2 with PLANNED 14.50, got: %v", eRt26Bay2)
	}

	// 2. CIP occupation window blocking Bay 2
	eCipBay2, ok := edgeMap["e-cip-bay2"]
	if !ok || eCipBay2["label"] != "OCCUPIES 14.10 - 15.20" || eCipBay2["source"] != "cip-b2" || eCipBay2["target"] != "bay-2" {
		t.Fatalf("expected CIP occupation interval on Bay 2, got: %v", eCipBay2)
	}

	// 3. Lab release timestamp
	eLabRt26, ok := edgeMap["e-lab-rt26"]
	if !ok || eLabRt26["label"] != "RELEASED 14.18" || eLabRt26["source"] != "lab-gate" || eLabRt26["target"] != "rt-26" {
		t.Fatalf("expected lab release edge with timestamp, got: %v", eLabRt26)
	}

	// 4. Inactive branch annotation
	eAt2F4, ok := edgeMap["e-at2-f4"]
	if !ok || eAt2F4["label"] != "TIDAK MENGISI 13 Okt" {
		t.Fatalf("expected inactive branch annotation on e-at2-f4, got: %v", eAt2F4)
	}
}

func TestDiscrepancyAuditAskTargetsAndSemanticLabels(t *testing.T) {
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	view := s.View()
	nodeMap := make(map[string]map[string]any)
	labelToID := make(map[string]string)
	for _, n := range view["nodes"].([]any) {
		nm := n.(map[string]any)
		id := nm["id"].(string)
		lbl := nm["label"].(string)
		nodeMap[id] = nm
		labelToID[lbl] = id
	}

	sc := scenario(t)

	// Audit 1: Ask target references business labels, not graph node IDs
	// e.g. "FG-1013-07" vs "fg-07", "RT-04" vs "rt-04", "Bay 2" vs "bay-2"
	businessTargets := []struct {
		businessLabel  string
		expectedNodeID string
	}{
		{"FG-1013-07", "fg-07"},
		{"RT-04", "rt-04"},
		{"Bay 2", "bay-2"},
	}
	for _, bt := range businessTargets {
		if _, ok := nodeMap[bt.businessLabel]; ok {
			t.Errorf("business label %s should not be a raw node id", bt.businessLabel)
		}
		resolvedID, ok := labelToID[bt.businessLabel]
		if !ok || resolvedID != bt.expectedNodeID {
			t.Errorf("business label %s resolved to %s, expected %s", bt.businessLabel, resolvedID, bt.expectedNodeID)
		}
	}

	// Audit 2: backward answer mix IDs are business labels ("RT-04", etc.)
	answers := sc["answers"].([]any)
	for _, aRaw := range answers {
		a := aRaw.(map[string]any)
		if a["id"] == "backward" {
			mix := a["mix"].([]any)
			for _, mRaw := range mix {
				m := mRaw.(map[string]any)
				mID := m["id"].(string)
				// mID is "RT-04", not "rt-04"
				if _, ok := nodeMap[mID]; ok {
					t.Errorf("mix id %s should be business label, but collided with node id", mID)
				}
				if _, ok := labelToID[mID]; !ok {
					t.Errorf("mix id %s cannot be resolved to any graph node label", mID)
				}
			}
		}
		if a["id"] == "forward" {
			lots := a["lots"].([]any)
			for _, lRaw := range lots {
				l := lRaw.(map[string]any)
				lotID := l["id"].(string)
				// lotID is "FG-1013-05", not "fg-05"
				if _, ok := nodeMap[lotID]; ok {
					t.Errorf("lot id %s should be business label, but collided with node id", lotID)
				}
				if _, ok := labelToID[lotID]; !ok {
					t.Errorf("lot id %s cannot be resolved to any graph node label", lotID)
				}
			}
		}
	}

	// Audit 3: handoffs from/to refer to business labels or composite stages
	// e.g. "TH-1, SP-1", "ST-4 + PM-3", "UHT-2, AT-2"
	handoffs := sc["handoffs"].([]any)
	for _, hRaw := range handoffs {
		h := hRaw.(map[string]any)
		for _, endpoint := range []string{h["from"].(string), h["to"].(string)} {
			if _, ok := labelToID[endpoint]; ok {
				continue // Direct match to graph node label
			}
			// Composite match (stages combined with ',' or '+')
			cleaned := strings.ReplaceAll(endpoint, "+", ",")
			parts := strings.Split(cleaned, ",")
			if len(parts) < 2 {
				t.Errorf("handoff endpoint %s cannot be resolved directly or as composite", endpoint)
			}
			for _, part := range parts {
				trimmed := strings.TrimSpace(part)
				if _, ok := labelToID[trimmed]; !ok {
					t.Errorf("composite handoff part %s in %s cannot be resolved to graph node label", trimmed, endpoint)
				}
			}
		}
	}
}
