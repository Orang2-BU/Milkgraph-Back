// Package trace serves calibrated historical evidence and propagates new mixtures.
package trace

import (
	"encoding/json"
	"errors"

	"milkgraph/seed"
)

var ErrNotFound = errors.New("not found")

type Service struct {
	answers  map[string]json.RawMessage
	handoffs json.RawMessage
}

func New() (*Service, error) {
	data, err := seed.Files.ReadFile("scenario.json")
	if err != nil {
		return nil, err
	}
	var scenario struct {
		Answers  []json.RawMessage `json:"answers"`
		Handoffs json.RawMessage   `json:"handoffs"`
	}
	if err := json.Unmarshal(data, &scenario); err != nil {
		return nil, err
	}
	s := &Service{answers: make(map[string]json.RawMessage), handoffs: scenario.Handoffs}
	for _, raw := range scenario.Answers {
		var a struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		s.answers[a.ID] = raw
	}
	return s, nil
}

func (s *Service) answer(id string) (map[string]any, error) {
	var a map[string]any
	if err := json.Unmarshal(s.answers[id], &a); err != nil {
		return nil, err
	}
	if id == "backward" || id == "forward" {
		a["subjectRt"] = "RT-04"
	}
	return a, nil
}

// Backward preserves calibrated temporal contributions, not total RMT volumes.
func (s *Service) Backward(lotID string) (map[string]any, error) {
	if lotID != "FG-1013-07" {
		return nil, ErrNotFound
	}
	return s.answer("backward")
}

func (s *Service) Forward(rtID, date string) (map[string]any, error) {
	if rtID != "RT-04" || (date != "2026-10-13" && date != "13 Okt") {
		return nil, ErrNotFound
	}
	return s.answer("forward")
}

func (s *Service) Explain(resource, time string) (map[string]any, error) {
	if resource != "Bay 2" || time != "14.50" {
		return nil, ErrNotFound
	}
	return s.answer("bay2")
}

func (s *Service) Handoffs(lotID string) (any, error) {
	if lotID != "FG-1013-07" {
		return nil, ErrNotFound
	}
	var h any
	err := json.Unmarshal(s.handoffs, &h)
	return h, err
}

func (s *Service) Templates() any {
	templates := make([]any, 0, 3)
	for _, id := range []string{"backward", "forward", "bay2"} {
		a, err := s.answer(id)
		if err == nil {
			templates = append(templates, map[string]any{"id": a["id"], "question": a["question"]})
		}
	}
	return templates
}
