package api

import (
	"context"
	"encoding/json"
	"milkgraph/internal/domain"
	"milkgraph/internal/rules"
	"milkgraph/internal/store"
)

// Bootstrap imports historical audit once; restarting never resets durable state.
func (s *Server) Bootstrap(ctx context.Context) error {
	if err := s.dbRequired(); err != nil {
		return err
	}
	seeds, err := LoadAllSeeds()
	if err != nil {
		return err
	}
	logs := []json.RawMessage{}
	for _, x := range arr(seeds.Ops["pastLog"]) {
		b, e := json.Marshal(x)
		if e != nil {
			return e
		}
		logs = append(logs, b)
	}
	return s.DB.SeedAudit(ctx, logs)
}
func (s *Server) state(ctx context.Context, entity, id string, target any) error {
	if s.DB == nil {
		return nil
	}
	raw, err := s.DB.Get(ctx, entity, id)
	if err != nil {
		return err
	}
	if len(raw) > 0 {
		return json.Unmarshal(raw, target)
	}
	return nil
}
func (s *Server) limits(ctx context.Context) (rules.Limits, error) {
	l := rules.DefaultLimits
	err := s.state(ctx, "limits", "current", &l)
	return l, err
}
func (s *Server) change(ctx context.Context, entity, id string, fn func(*store.Tx, json.RawMessage) (store.Change, error)) (any, error) {
	if err := s.dbRequired(); err != nil {
		return nil, err
	}
	c, err := s.DB.Update(ctx, entity, id, fn)
	if err != nil {
		return nil, err
	}
	return map[string]any{"result": c.Value, "log": c.Log}, nil
}
func roleLabel(role string) string {
	if def, ok := domain.RolesMap[domain.RoleId(role)]; ok {
		return def.Label
	}
	return role
}

func auditEntry(a *AuthContext, kind, title, detail, note, tone string) map[string]any {
	return map[string]any{"kind": kind, "title": title, "detail": detail, "note": note, "by": a.Name, "role": roleLabel(a.Role), "at": "14.42", "tone": tone}
}
