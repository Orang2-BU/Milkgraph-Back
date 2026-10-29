package api

import (
	"encoding/json"
	"fmt"
	"sync"

	"milkgraph/seed"
)

type SeedData struct {
	Scenario map[string]any
	Ops      map[string]any
	Site     map[string]any
	Roles    map[string]any
	Limits   map[string]any
	Golden   map[string]any
	mu       sync.RWMutex
}

func loadJSON(filename string) (map[string]any, error) {
	raw, err := seed.Files.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filename, err)
	}
	var res map[string]any
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", filename, err)
	}
	return res, nil
}

func LoadAllSeeds() (*SeedData, error) {
	scenario, err := loadJSON("scenario.json")
	if err != nil {
		return nil, err
	}
	ops, err := loadJSON("ops.json")
	if err != nil {
		return nil, err
	}
	site, err := loadJSON("site.json")
	if err != nil {
		return nil, err
	}
	roles, err := loadJSON("roles.json")
	if err != nil {
		return nil, err
	}
	limits, err := loadJSON("limits.json")
	if err != nil {
		return nil, err
	}
	golden, err := loadJSON("golden-rules.json")
	if err != nil {
		return nil, err
	}

	return &SeedData{
		Scenario: scenario,
		Ops:      ops,
		Site:     site,
		Roles:    roles,
		Limits:   limits,
		Golden:   golden,
	}, nil
}
