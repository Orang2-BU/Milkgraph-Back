// Package graph serves validated, read-only context snapshots, not a material-flow engine.
package graph

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"milkgraph/seed"
	"strings"
)

var ErrNotFound = errors.New("graph node not found")
var ErrValidation = errors.New("invalid graph")

const MaxDepth = 32

const nodeTypes = " Farm RT LabResult CIPEvent Bay Buffer Thermal Separator Storage Cream Premix Mixing Sterilisasi Aseptic Filling Lot "

type Service struct {
	view    map[string]any
	sources map[string]bool
}

func New() (*Service, error) {
	b, err := seed.Files.ReadFile("scenario.json")
	if err != nil {
		return nil, err
	}
	var scenario struct {
		Graph   map[string]any `json:"graph"`
		Sources []struct {
			ID string `json:"id"`
		} `json:"sources"`
	}
	if err = json.Unmarshal(b, &scenario); err != nil {
		return nil, err
	}
	sources := map[string]bool{}
	for _, v := range scenario.Sources {
		sources[v.ID] = true
	}
	if err = validate(scenario.Graph, sources); err != nil {
		return nil, err
	}
	return &Service{view: scenario.Graph, sources: sources}, nil
}

func text(v any) string        { s, _ := v.(string); return s }
func bad(message string) error { return fmt.Errorf("%w: %s", ErrValidation, message) }

func validate(v map[string]any, sources map[string]bool) error {
	nodes, ok := v["nodes"].([]any)
	if !ok {
		return bad("nodes must be an array")
	}
	edges, ok := v["edges"].([]any)
	if !ok {
		return bad("edges must be an array")
	}
	ids := map[string]bool{}
	for _, raw := range nodes {
		n, ok := raw.(map[string]any)
		if !ok {
			return bad("node must be an object")
		}
		id := text(n["id"])
		if strings.TrimSpace(id) == "" || ids[id] {
			return bad("empty or duplicate node ID: " + id)
		}
		ids[id] = true
		typ := text(n["type"])
		if typ == "" || !strings.Contains(nodeTypes, " "+typ+" ") {
			return bad("unknown node type: " + typ)
		}
		if strings.TrimSpace(text(n["label"])) == "" {
			return bad("missing label: " + id)
		}
		if !sources[text(n["source"])] {
			return bad("unknown provenance source: " + text(n["source"]))
		}
		var rank float64
		switch x := n["rank"].(type) {
		case float64:
			rank = x
		case int:
			rank = float64(x)
		default:
			return bad("rank must be an integer: " + id)
		}
		if math.IsNaN(rank) || math.IsInf(rank, 0) || rank < 0 || math.Trunc(rank) != rank {
			return bad("invalid rank: " + id)
		}
	}
	edgeIDs := map[string]bool{}
	for _, raw := range edges {
		e, ok := raw.(map[string]any)
		if !ok {
			return bad("edge must be an object")
		}
		id := text(e["id"])
		if strings.TrimSpace(id) == "" || edgeIDs[id] {
			return bad("empty or duplicate edge ID: " + id)
		}
		edgeIDs[id] = true
		if !ids[text(e["source"])] || !ids[text(e["target"])] {
			return bad("unknown edge endpoint: " + id)
		}
		if strings.TrimSpace(text(e["label"])) == "" {
			return bad("missing edge label: " + id)
		}
		if p, ok := e["planned"]; ok {
			if _, ok := p.(bool); !ok {
				return bad("planned must be boolean: " + id)
			}
		}
	}
	return nil
}

// Subgraph follows outgoing edges only, including contextual/planned relations.
// These are evidence connections, not proof that a material transfer occurred.
// Results retain snapshot order; cycles are visited once; depth is 0..MaxDepth.
func (s *Service) Subgraph(node string, depth int) (map[string]any, error) {
	if depth < 0 || depth > MaxDepth {
		return nil, bad("depth must be 0..32")
	}
	found := false
	for _, raw := range s.view["nodes"].([]any) {
		if text(raw.(map[string]any)["id"]) == node {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrNotFound
	}
	reached := map[string]int{node: 0}
	queue := []string{node}
	edgeIDs := map[string]bool{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if reached[id] >= depth {
			continue
		}
		for _, raw := range s.view["edges"].([]any) {
			e := raw.(map[string]any)
			if text(e["source"]) != id {
				continue
			}
			edgeIDs[text(e["id"])] = true
			target := text(e["target"])
			if _, ok := reached[target]; !ok {
				reached[target] = reached[id] + 1
				queue = append(queue, target)
			}
		}
	}
	out := map[string]any{"nodes": []any{}, "edges": []any{}}
	for _, raw := range s.view["nodes"].([]any) {
		if _, ok := reached[text(raw.(map[string]any)["id"])]; ok {
			out["nodes"] = append(out["nodes"].([]any), copyValue(raw))
		}
	}
	for _, raw := range s.view["edges"].([]any) {
		if edgeIDs[text(raw.(map[string]any)["id"])] {
			out["edges"] = append(out["edges"].([]any), copyValue(raw))
		}
	}
	return out, nil
}

// Project upserts complete node/edge records by ID into a new snapshot.
// Existing order is retained, additions use input order. It never infers links,
// changes the receiver, or persists data. Provenance must exist in seed sources.
func (s *Service) Project(delta map[string]any) (*Service, error) {
	if delta == nil {
		return nil, bad("nil projection")
	}
	out := s.View()
	for _, kind := range []string{"nodes", "edges"} {
		raw, exists := delta[kind]
		if !exists {
			continue
		}
		items, ok := raw.([]any)
		if !ok {
			return nil, bad(kind + " must be an array")
		}
		base := out[kind].([]any)
		positions := map[string]int{}
		for i, v := range base {
			positions[text(v.(map[string]any)["id"])] = i
		}
		seen := map[string]bool{}
		for _, v := range items {
			m, ok := v.(map[string]any)
			if !ok {
				return nil, bad(kind + " record must be an object")
			}
			id := text(m["id"])
			if id == "" || seen[id] {
				return nil, bad("empty or duplicate projection ID: " + id)
			}
			seen[id] = true
			if i, ok := positions[id]; ok {
				base[i] = copyValue(m)
			} else {
				base = append(base, copyValue(m))
			}
		}
		out[kind] = base
	}
	if err := validate(out, s.sources); err != nil {
		return nil, err
	}
	return &Service{view: out, sources: s.sources}, nil
}

// View returns a deep copy; callers never share mutable state with the service.
func (s *Service) View() map[string]any { return copyValue(s.view).(map[string]any) }

func copyValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[k] = copyValue(v)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = copyValue(v)
		}
		return out
	default:
		return v
	}
}
