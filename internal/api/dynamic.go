package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"milkgraph/internal/domain"
	"milkgraph/internal/rules"
)

type DynamicState struct {
	mu           sync.RWMutex
	currentRole  string
	limits       rules.Limits
	decisions    map[string]domain.PlanDecision
	acks         map[string]domain.InstructionView
	demandSend   *domain.DemandSendRecord
	labDecisions map[string]domain.LabDecision
	holds        []domain.Hold
	nextHoldNum  int
	auditLogs    []map[string]any
}

func (s *Server) initDynamic() {
	if s.dyn != nil {
		return
	}
	d := &DynamicState{
		currentRole:  "planner",
		limits:       rules.DefaultLimits,
		decisions:    make(map[string]domain.PlanDecision),
		acks:         make(map[string]domain.InstructionView),
		labDecisions: make(map[string]domain.LabDecision),
		holds:        make([]domain.Hold, 0),
		nextHoldNum:  1,
		auditLogs:    make([]map[string]any, 0),
	}
	// Seed initial past holds & past audit logs
	if past, ok := s.seeds.Ops["pastHolds"].([]any); ok {
		for _, item := range past {
			raw, _ := json.Marshal(item)
			var h domain.Hold
			if err := json.Unmarshal(raw, &h); err == nil {
				d.holds = append(d.holds, h)
			}
		}
	}
	if logs, ok := s.seeds.Ops["pastLog"].([]any); ok {
		for _, item := range logs {
			if m, ok := item.(map[string]any); ok {
				d.auditLogs = append(d.auditLogs, m)
			}
		}
	}
	s.dyn = d
}

func (s *Server) resetDemo(ctx context.Context) error {
	s.dyn.mu.Lock()
	defer s.dyn.mu.Unlock()

	s.dyn.currentRole = "planner"
	s.dyn.limits = rules.DefaultLimits
	s.dyn.decisions = make(map[string]domain.PlanDecision)
	s.dyn.acks = make(map[string]domain.InstructionView)
	s.dyn.demandSend = nil
	s.dyn.labDecisions = make(map[string]domain.LabDecision)
	s.dyn.holds = make([]domain.Hold, 0)
	s.dyn.nextHoldNum = 1
	s.dyn.auditLogs = make([]map[string]any, 0)

	// Re-seed past holds & past logs
	if past, ok := s.seeds.Ops["pastHolds"].([]any); ok {
		for _, item := range past {
			raw, _ := json.Marshal(item)
			var h domain.Hold
			if err := json.Unmarshal(raw, &h); err == nil {
				s.dyn.holds = append(s.dyn.holds, h)
			}
		}
	}
	if logs, ok := s.seeds.Ops["pastLog"].([]any); ok {
		for _, item := range logs {
			if m, ok := item.(map[string]any); ok {
				s.dyn.auditLogs = append(s.dyn.auditLogs, m)
			}
		}
	}

	if s.DB != nil {
		if err := s.DB.Reset(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) addAuditLog(entry map[string]any) {
	s.dyn.mu.Lock()
	defer s.dyn.mu.Unlock()
	entry["id"] = len(s.dyn.auditLogs) + 1
	s.dyn.auditLogs = append([]map[string]any{entry}, s.dyn.auditLogs...)
}

func (s *Server) registerDynamic(register func(string, string, func(*http.Request) (any, error)), mux *http.ServeMux) {
	s.initDynamic()

	// 1. Session Role
	register("POST /api/v1/session/role", "", func(r *http.Request) (any, error) {
		var req struct {
			Role string `json:"role"`
		}
		if err := decode(r, &req); err != nil {
			return nil, err
		}
		roleId := domain.RoleId(req.Role)
		def, ok := domain.RolesMap[roleId]
		if !ok {
			return nil, invalid("Role tidak valid.", "role")
		}
		s.dyn.mu.Lock()
		s.dyn.currentRole = req.Role
		s.dyn.mu.Unlock()

		perms := domain.PermissionsForRole(roleId)
		permStrs := make([]string, len(perms))
		for i, p := range perms {
			permStrs[i] = string(p)
		}
		sort.Strings(permStrs)

		session := map[string]any{
			"user": map[string]any{
				"name":     def.User,
				"role":     def.Label,
				"initials": def.Initials,
				"shift":    "Shift Siang",
			},
			"role":        req.Role,
			"permissions": permStrs,
		}
		return map[string]any{"result": session, "log": nil}, nil
	})

	// 2. Notifications
	register("GET /api/v1/notifications", "", func(r *http.Request) (any, error) {
		planCurrent := s.plan("current")
		var p rules.Plan
		raw, _ := json.Marshal(planCurrent)
		_ = json.Unmarshal(raw, &p)

		s.dyn.mu.RLock()
		curLimits := s.dyn.limits
		dec, isApproved := s.dyn.decisions["current"]
		s.dyn.mu.RUnlock()

		checkRes := rules.EvaluateResult(p, curLimits)
		planSubtitle := fmt.Sprintf("%d constraint dilanggar · starvation %d menit", checkRes.Violations.Before, checkRes.Starvation.Before)
		planTone := "crit"
		if isApproved && dec.Status == "approved" {
			planSubtitle = "Rekomendasi disetujui · RT-26 dialihkan ke Bay 1"
			planTone = "ok"
		}

		holdingLeft := checkRes.HoldingLeft
		holdingSubtitle := fmt.Sprintf("Tersisa %dj %02dm · menunggu bay", holdingLeft/60, holdingLeft%60)

		notifs := []map[string]any{
			{
				"id":       "PLAN-FIFO",
				"title":    "Rencana shift tidak layak",
				"subtitle": planSubtitle,
				"sourceId": "PLAN-FIFO",
				"tone":     planTone,
			},
			{
				"id":       "HOLD-RT26",
				"title":    "RT-26 mendekati batas holding time",
				"subtitle": holdingSubtitle,
				"sourceId": "HOLD-RT26",
				"tone":     "crit",
			},
			{
				"id":       "LAB-8812",
				"title":    "QC FG out-of-spec FG-1013-07",
				"subtitle": "Residu antibiotik · jalankan trace",
				"sourceId": "LAB-8812",
				"tone":     "crit",
			},
		}
		return notifs, nil
	})

	// 3. Demo Reset
	register("POST /api/v1/demo/reset", "", func(r *http.Request) (any, error) {
		if err := s.resetDemo(r.Context()); err != nil {
			return nil, err
		}
		return map[string]any{"result": map[string]any{"ok": true}, "log": nil}, nil
	})

	// 4. Receiving
	register("GET /api/v1/receiving", "", func(r *http.Request) (any, error) {
		s.dyn.mu.RLock()
		curLimits := s.dyn.limits
		s.dyn.mu.RUnlock()

		holdingLeft := rules.HoldingLeft(curLimits)

		stages := clone(arr(s.seeds.Scenario["stages"]))
		tankers := clone(arr(s.seeds.Scenario["tankers"]))
		assets := clone(arr(s.seeds.Scenario["assets"]))
		metrics := clone(arr(s.seeds.Scenario["metrics"]))
		labHist := clone(arr(s.seeds.Scenario["labHistory"]))

		// Cek apakah ada mutasi keputusan lab
		s.dyn.mu.RLock()
		for _, t := range tankers {
			tm := obj(t)
			rtId := str(tm["id"])
			for _, dec := range s.dyn.labDecisions {
				if dec.Status == "dirilis" && strings.Contains(dec.Note, rtId) {
					tm["stage"] = "Released"
				}
			}
		}
		s.dyn.mu.RUnlock()

		resp := map[string]any{
			"metrics":    metrics,
			"stages":     stages,
			"tankers":    tankers,
			"assets":     assets,
			"labHistory": labHist,
			"labSummary": map[string]any{
				"avgMinutes":   52,
				"deltaMinutes": 14,
				"waiting":      3,
				"sourceId":     "LAB-QUEUE",
			},
			"incident": map[string]any{
				"planId":         "current",
				"rt":             "RT-26",
				"holdingLeftMin": holdingLeft,
				"summary":        "Antrean RT menumpuk karena lab test lama dan Bay 2 sedang CIP.",
				"factors": []map[string]any{
					{"label": "Lab test 52 menit", "sourceId": "LAB-QUEUE"},
					{"label": "Bay 2 CIP sampai 15.20", "sourceId": "CIP-B2"},
					{"label": "Holding time RT-26", "sourceId": "HOLD-RT26"},
				},
			},
		}
		return resp, nil
	})

	// 5. Plans Check
	register("POST /api/v1/plans/{id}/check", "", func(r *http.Request) (any, error) {
		pMap := s.plan(r.PathValue("id"))
		if pMap == nil {
			return nil, missing()
		}
		var p rules.Plan
		raw, _ := json.Marshal(pMap)
		_ = json.Unmarshal(raw, &p)

		s.dyn.mu.RLock()
		lim := s.dyn.limits
		s.dyn.mu.RUnlock()

		if r.Body != nil && r.ContentLength > 0 {
			var body struct {
				Limits *rules.Limits `json:"limits"`
			}
			if err := decode(r, &body); err == nil && body.Limits != nil {
				lim = *body.Limits
			}
		}

		res := rules.EvaluateResult(p, lim)
		return res, nil
	})

	// 6. Plans Solve
	register("POST /api/v1/plans/{id}/solve", "plan.decide", func(r *http.Request) (any, error) {
		pMap := s.plan(r.PathValue("id"))
		if pMap == nil {
			return nil, missing()
		}
		return map[string]any{"result": s.planView(pMap), "log": nil}, nil
	})

	// 7. Decisions List
	register("GET /api/v1/decisions", "", func(r *http.Request) (any, error) {
		s.dyn.mu.RLock()
		defer s.dyn.mu.RUnlock()
		list := make([]domain.PlanDecision, 0, len(s.dyn.decisions))
		for _, d := range s.dyn.decisions {
			list = append(list, d)
		}
		return list, nil
	})

	// 8. Plans Decision Mutate
	register("POST /api/v1/plans/{id}/decision", "plan.decide", func(r *http.Request) (any, error) {
		auth, _ := s.authenticate(r)
		planId := r.PathValue("id")
		pMap := s.plan(planId)
		if pMap == nil {
			return nil, missing()
		}

		var req struct {
			Status string `json:"status"`
			Note   string `json:"note"`
		}
		if err := decode(r, &req); err != nil {
			return nil, err
		}
		if req.Status != "approved" && req.Status != "rejected" {
			return nil, invalid("Status harus approved atau rejected.", "status")
		}
		if len(req.Note) > 500 {
			return nil, invalid("Catatan maksimal 500 karakter.", "note")
		}
		if req.Status == "rejected" && strings.TrimSpace(req.Note) == "" {
			return nil, invalid("Catatan wajib diisi untuk penolakan.", "note")
		}

		s.dyn.mu.Lock()
		if _, exists := s.dyn.decisions[planId]; exists {
			s.dyn.mu.Unlock()
			return nil, conflict()
		}

		nowClock := "14.42"
		dec := domain.PlanDecision{
			Plan:   planId,
			Status: req.Status,
			Note:   req.Note,
			By:     auth.Name,
			At:     nowClock,
		}
		s.dyn.decisions[planId] = dec
		s.dyn.mu.Unlock()

		title := "Rekomendasi disetujui"
		tone := "ok"
		if req.Status == "rejected" {
			title = "Rekomendasi ditolak"
			tone = "warn"
		}
		logEntry := map[string]any{
			"kind":   "rencana",
			"title":  title,
			"detail": str(pMap["label"]),
			"note":   req.Note,
			"by":     auth.Name,
			"role":   domain.RolesMap[domain.RoleId(auth.Role)].Label,
			"at":     nowClock,
			"tone":   tone,
		}
		s.addAuditLog(logEntry)

		return map[string]any{"result": dec, "log": logEntry}, nil
	})

	// 9. Limits GET & PUT
	register("GET /api/v1/limits", "", func(r *http.Request) (any, error) {
		s.dyn.mu.RLock()
		defer s.dyn.mu.RUnlock()
		return s.dyn.limits, nil
	})

	register("PUT /api/v1/limits", "constraint.edit", func(r *http.Request) (any, error) {
		auth, _ := s.authenticate(r)
		var newL rules.Limits
		if err := decode(r, &newL); err != nil {
			return nil, err
		}
		if err := validateLimits(newL); err != nil {
			return nil, err
		}

		s.dyn.mu.Lock()
		oldL := s.dyn.limits
		s.dyn.limits = newL
		s.dyn.mu.Unlock()

		diffText := formatLimitsDiff(oldL, newL)
		nowClock := "14.42"
		logEntry := map[string]any{
			"kind":   "constraint",
			"title":  "Batas constraint diubah",
			"detail": diffText,
			"note":   "",
			"by":     auth.Name,
			"role":   domain.RolesMap[domain.RoleId(auth.Role)].Label,
			"at":     nowClock,
			"tone":   "ok",
		}
		s.addAuditLog(logEntry)

		return map[string]any{"result": newL, "log": logEntry}, nil
	})

	// 10. Instructions
	register("GET /api/v1/plans/{id}/instructions", "", func(r *http.Request) (any, error) {
		planId := r.PathValue("id")
		pMap := s.plan(planId)
		if pMap == nil {
			return nil, missing()
		}

		s.dyn.mu.RLock()
		dec, isApproved := s.dyn.decisions[planId]
		acks := s.dyn.acks
		s.dyn.mu.RUnlock()

		if !isApproved || dec.Status != "approved" {
			return []any{}, nil
		}

		var p rules.Plan
		raw, _ := json.Marshal(pMap)
		_ = json.Unmarshal(raw, &p)

		instructions := rules.Instructions(p)
		views := make([]domain.InstructionView, 0, len(instructions))
		for _, ins := range instructions {
			v := domain.InstructionView{
				ID:   ins.ID,
				Role: ins.Role,
				Text: ins.Text,
				From: ins.From,
			}
			if ack, ok := acks[ins.ID]; ok {
				v.AckedBy = ack.AckedBy
				v.AckedAt = ack.AckedAt
			}
			views = append(views, v)
		}
		return views, nil
	})

	register("POST /api/v1/instructions/{id}/ack", "instruction.ack", func(r *http.Request) (any, error) {
		auth, _ := s.authenticate(r)
		insID := r.PathValue("id")

		s.dyn.mu.RLock()
		dec, isApproved := s.dyn.decisions["current"]
		s.dyn.mu.RUnlock()

		if !isApproved || dec.Status != "approved" {
			return nil, missing()
		}

		var p rules.Plan
		raw, _ := json.Marshal(s.plan("current"))
		_ = json.Unmarshal(raw, &p)

		instructions := rules.Instructions(p)
		var target *rules.Instruction
		for _, ins := range instructions {
			if ins.ID == insID {
				target = &ins
				break
			}
		}
		if target == nil {
			return nil, missing()
		}

		// Role match check
		if auth.Role != target.Role {
			return nil, &apiError{
				status:  403,
				code:    "FORBIDDEN",
				message: fmt.Sprintf("Hanya %s yang dapat melakukan ini.", domain.RolesMap[domain.RoleId(target.Role)].Label),
			}
		}

		s.dyn.mu.Lock()
		if _, already := s.dyn.acks[insID]; already {
			s.dyn.mu.Unlock()
			return nil, conflict()
		}

		nowClock := "14.42"
		view := domain.InstructionView{
			ID:      target.ID,
			Role:    target.Role,
			Text:    target.Text,
			From:    target.From,
			AckedBy: auth.Name,
			AckedAt: nowClock,
		}
		s.dyn.acks[insID] = view
		s.dyn.mu.Unlock()

		logEntry := map[string]any{
			"kind":   "instruksi",
			"title":  "Instruksi dikonfirmasi",
			"detail": target.Text,
			"note":   "",
			"by":     auth.Name,
			"role":   domain.RolesMap[domain.RoleId(auth.Role)].Label,
			"at":     nowClock,
			"tone":   "ok",
		}
		s.addAuditLog(logEntry)

		return map[string]any{"result": view, "log": logEntry}, nil
	})

	// 11. Supply
	register("GET /api/v1/supply/demand-sends", "", func(r *http.Request) (any, error) {
		s.dyn.mu.RLock()
		defer s.dyn.mu.RUnlock()
		if s.dyn.demandSend == nil {
			return map[string]any{"sent": false}, nil
		}
		return map[string]any{
			"sent":   true,
			"at":     s.dyn.demandSend.At,
			"by":     s.dyn.demandSend.By,
			"detail": s.dyn.demandSend.Detail,
		}, nil
	})

	register("POST /api/v1/supply/demand/send", "demand.send", func(r *http.Request) (any, error) {
		auth, _ := s.authenticate(r)
		s.dyn.mu.Lock()
		if s.dyn.demandSend != nil && s.dyn.demandSend.Sent {
			s.dyn.mu.Unlock()
			return nil, conflict()
		}

		nowClock := "14.42"
		detail := "278.659 L untuk 6 SKU, 29 RT dari 5 farm"
		rec := domain.DemandSendRecord{
			Sent:   true,
			At:     nowClock,
			By:     auth.Name,
			Detail: detail,
		}
		s.dyn.demandSend = &rec
		s.dyn.mu.Unlock()

		logEntry := map[string]any{
			"kind":   "rencana",
			"title":  "Kebutuhan susu dikirim ke farm",
			"detail": detail,
			"note":   "",
			"by":     auth.Name,
			"role":   domain.RolesMap[domain.RoleId(auth.Role)].Label,
			"at":     nowClock,
			"tone":   "ok",
		}
		s.addAuditLog(logEntry)

		return map[string]any{"result": rec, "log": logEntry}, nil
	})

	// 12. Lab
	register("GET /api/v1/lab/results", "", func(r *http.Request) (any, error) {
		s.dyn.mu.RLock()
		decisions := s.dyn.labDecisions
		s.dyn.mu.RUnlock()

		kindFilter := r.URL.Query().Get("kind")
		all := arr(s.seeds.Ops["labResults"])
		res := make([]any, 0, len(all))
		for _, x := range all {
			m := clone(obj(x))
			if kindFilter != "" && str(m["kind"]) != kindFilter {
				continue
			}
			id := str(m["id"])
			if dec, ok := decisions[id]; ok {
				m["decision"] = dec
			}
			if str(m["status"]) == "uji-ulang" {
				m["eta"] = "15.30"
			}
			res = append(res, m)
		}
		return res, nil
	})

	register("GET /api/v1/lab/summary", "", func(r *http.Request) (any, error) {
		return map[string]any{
			"waiting":    3,
			"released":   3,
			"avgWaitMin": 52,
		}, nil
	})

	register("POST /api/v1/lab/results/{id}/decision", "lab.decide", func(r *http.Request) (any, error) {
		auth, _ := s.authenticate(r)
		labId := r.PathValue("id")
		labItem := item(s.seeds.Ops["labResults"], labId)
		if labItem == nil {
			return nil, missing()
		}

		if str(labItem["kind"]) != "penerimaan" {
			return nil, invalid("Hanya hasil lab penerimaan yang dapat diputuskan.", "kind")
		}

		var req struct {
			Status string `json:"status"`
			Note   string `json:"note"`
		}
		if err := decode(r, &req); err != nil {
			return nil, err
		}
		if req.Status != "dirilis" && req.Status != "ditolak" {
			return nil, invalid("Status harus dirilis atau ditolak.", "status")
		}

		curStatus := str(labItem["status"])
		if curStatus == "uji-ulang" && req.Status == "dirilis" {
			return nil, invalid("Rilis menunggu hasil uji ulang.", "status")
		}
		if req.Status == "ditolak" && strings.TrimSpace(req.Note) == "" {
			return nil, invalid("Catatan wajib diisi untuk penolakan.", "note")
		}

		s.dyn.mu.Lock()
		if _, exists := s.dyn.labDecisions[labId]; exists {
			s.dyn.mu.Unlock()
			return nil, conflict()
		}

		nowClock := "14.42"
		dec := domain.LabDecision{
			Status: req.Status,
			Note:   req.Note,
			By:     auth.Name,
			At:     nowClock,
		}
		s.dyn.labDecisions[labId] = dec
		s.dyn.mu.Unlock()

		tone := "ok"
		if req.Status == "ditolak" {
			tone = "crit"
		}
		logEntry := map[string]any{
			"kind":   "lab",
			"title":  fmt.Sprintf("Hasil lab %s %s", str(labItem["subject"]), req.Status),
			"detail": labId,
			"note":   req.Note,
			"by":     auth.Name,
			"role":   domain.RolesMap[domain.RoleId(auth.Role)].Label,
			"at":     nowClock,
			"tone":   tone,
		}
		s.addAuditLog(logEntry)

		labView := clone(labItem)
		labView["decision"] = dec
		return map[string]any{"result": labView, "log": logEntry}, nil
	})

	// 13. Holds
	register("GET /api/v1/holds", "", func(r *http.Request) (any, error) {
		s.dyn.mu.RLock()
		defer s.dyn.mu.RUnlock()
		// Reverse order (newest first)
		res := make([]domain.Hold, len(s.dyn.holds))
		for i, h := range s.dyn.holds {
			res[len(s.dyn.holds)-1-i] = h
		}
		return res, nil
	})

	register("POST /api/v1/holds", "hold.submit", func(r *http.Request) (any, error) {
		auth, _ := s.authenticate(r)
		var req struct {
			Trigger string       `json:"trigger"`
			RT      string       `json:"rt"`
			Lots    []domain.Lot `json:"lots"`
			Note    string       `json:"note"`
		}
		if err := decode(r, &req); err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.Trigger) == "" {
			return nil, invalid("Pemicu hold wajib diisi.", "trigger")
		}
		if strings.TrimSpace(req.RT) == "" {
			return nil, invalid("RT sumber wajib diisi.", "rt")
		}

		s.dyn.mu.Lock()
		for _, h := range s.dyn.holds {
			if h.Trigger == req.Trigger && h.Status != "ditolak" {
				s.dyn.mu.Unlock()
				return nil, conflict()
			}
		}

		holdId := fmt.Sprintf("HOLD-%03d", s.dyn.nextHoldNum)
		s.dyn.nextHoldNum++

		nowClock := "14.42"
		h := domain.Hold{
			ID:      holdId,
			Trigger: req.Trigger,
			RT:      req.RT,
			Lots:    req.Lots,
			Note:    req.Note,
			Status:  "diajukan",
			By:      auth.Name,
			At:      nowClock,
		}
		s.dyn.holds = append(s.dyn.holds, h)
		s.dyn.mu.Unlock()

		logEntry := map[string]any{
			"kind":   "hold",
			"title":  "Daftar hold diajukan ke QA",
			"detail": fmt.Sprintf("%d lot dari %s, pemicu %s", len(req.Lots), req.RT, req.Trigger),
			"note":   req.Note,
			"by":     auth.Name,
			"role":   domain.RolesMap[domain.RoleId(auth.Role)].Label,
			"at":     nowClock,
			"tone":   "warn",
		}
		s.addAuditLog(logEntry)

		return map[string]any{"result": h, "log": logEntry}, nil
	})

	register("POST /api/v1/holds/{id}/review", "hold.review", func(r *http.Request) (any, error) {
		auth, _ := s.authenticate(r)
		holdId := r.PathValue("id")

		var req struct {
			Status string `json:"status"`
			Note   string `json:"note"`
		}
		if err := decode(r, &req); err != nil {
			return nil, err
		}
		if req.Status != "aktif" && req.Status != "ditolak" && req.Status != "dilepas" {
			return nil, invalid("Status review tidak valid.", "status")
		}
		if (req.Status == "ditolak" || req.Status == "dilepas") && strings.TrimSpace(req.Note) == "" {
			return nil, invalid("Catatan wajib diisi untuk penolakan atau pelepasan hold.", "note")
		}

		s.dyn.mu.Lock()
		var found *domain.Hold
		for i := range s.dyn.holds {
			if s.dyn.holds[i].ID == holdId {
				found = &s.dyn.holds[i]
				break
			}
		}
		if found == nil {
			s.dyn.mu.Unlock()
			return nil, missing()
		}

		// Validasi transisi status
		if found.Status == "diajukan" && req.Status != "aktif" && req.Status != "ditolak" {
			s.dyn.mu.Unlock()
			return nil, invalid("Hold diajukan hanya dapat disetujui aktif atau ditolak.", "status")
		}
		if found.Status == "aktif" && req.Status != "dilepas" {
			s.dyn.mu.Unlock()
			return nil, invalid("Hold aktif hanya dapat dilepas.", "status")
		}
		if found.Status == "ditolak" || found.Status == "dilepas" {
			s.dyn.mu.Unlock()
			return nil, invalid("Hold sudah selesai dan tidak dapat diubah lagi.", "status")
		}

		nowClock := "14.42"
		found.Status = req.Status
		found.Review = &domain.HoldReview{
			Note: req.Note,
			By:   auth.Name,
			At:   nowClock,
		}
		resHold := *found
		s.dyn.mu.Unlock()

		title := "Hold disetujui QA"
		tone := "crit"
		if req.Status == "ditolak" {
			title = "Pengajuan hold ditolak"
			tone = "neutral"
		} else if req.Status == "dilepas" {
			title = "Hold dilepas"
			tone = "ok"
		}

		logEntry := map[string]any{
			"kind":   "hold",
			"title":  title,
			"detail": holdId,
			"note":   req.Note,
			"by":     auth.Name,
			"role":   domain.RolesMap[domain.RoleId(auth.Role)].Label,
			"at":     nowClock,
			"tone":   tone,
		}
		s.addAuditLog(logEntry)

		return map[string]any{"result": resHold, "log": logEntry}, nil
	})

	register("GET /api/v1/holds/{id}/genealogy", "", func(r *http.Request) (any, error) {
		holdId := r.PathValue("id")
		s.dyn.mu.RLock()
		var found *domain.Hold
		for _, h := range s.dyn.holds {
			if h.ID == holdId {
				cpy := h
				found = &cpy
				break
			}
		}
		s.dyn.mu.RUnlock()
		if found == nil {
			return nil, missing()
		}

		lotLocs := obj(s.seeds.Ops["lotLocation"])
		lotsWithLoc := make([]map[string]any, 0, len(found.Lots))
		for _, lot := range found.Lots {
			lm := map[string]any{
				"id":      lot.ID,
				"product": lot.Product,
				"percent": lot.Percent,
				"packs":   lot.Packs,
				"place":   "Gudang FG, rak C-14",
				"shipped": false,
			}
			if loc, ok := lotLocs[lot.ID]; ok {
				locObj := obj(loc)
				lm["place"] = locObj["place"]
				lm["shipped"] = locObj["shipped"]
			}
			lotsWithLoc = append(lotsWithLoc, lm)
		}

		resp := map[string]any{
			"hold":     found,
			"handoffs": s.seeds.Scenario["handoffs"],
			"labTests": []map[string]any{
				{"id": "LAB-4471", "subject": "RT-04", "result": "Negatif (0.003 ppm)"},
				{"id": "LAB-8812", "subject": "FG-1013-07", "result": "Positif (>0.005 ppm)"},
			},
			"lots": lotsWithLoc,
		}
		return resp, nil
	})

	// 14. Data Sources
	register("GET /api/v1/data-sources", "", func(r *http.Request) (any, error) {
		return s.seeds.Ops["dataSources"], nil
	})

	register("POST /api/v1/data-sources/{id}/test", "", func(r *http.Request) (any, error) {
		ds := item(s.seeds.Ops["dataSources"], r.PathValue("id"))
		if ds == nil {
			return nil, missing()
		}
		return map[string]any{
			"ok":        true,
			"text":      "Koneksi berhasil",
			"latencyMs": 180,
		}, nil
	})

	register("POST /api/v1/data-sources/{id}/sync", "", func(r *http.Request) (any, error) {
		ds := item(s.seeds.Ops["dataSources"], r.PathValue("id"))
		if ds == nil {
			return nil, missing()
		}
		ds["lastSync"] = "14.42"
		return map[string]any{"result": ds, "log": nil}, nil
	})

	// 15. Site Status & Bottlenecks
	register("GET /api/v1/site/status", "", func(r *http.Request) (any, error) {
		view := r.URL.Query().Get("view")
		if view != "after" {
			view = "before"
		}
		st := clone(obj(obj(s.seeds.Site["status"])[view]))
		viewLabel := "Rencana awal · 14.42"
		if view == "after" {
			viewLabel = "Rekomendasi · proyeksi 15.30"
		}
		st["viewLabel"] = viewLabel
		return st, nil
	})

	register("GET /api/v1/site/bottlenecks", "", func(r *http.Request) (any, error) {
		view := r.URL.Query().Get("view")
		if view != "after" {
			view = "before"
		}
		return obj(s.seeds.Site["bottlenecks"])[view], nil
	})

	// 16. Audit & Audit CSV
	register("GET /api/v1/audit", "", func(r *http.Request) (any, error) {
		kindFilter := r.URL.Query().Get("kind")
		s.dyn.mu.RLock()
		defer s.dyn.mu.RUnlock()

		res := make([]map[string]any, 0, len(s.dyn.auditLogs))
		for _, log := range s.dyn.auditLogs {
			if kindFilter != "" && str(log["kind"]) != kindFilter {
				continue
			}
			res = append(res, log)
		}
		return res, nil
	})

	mux.HandleFunc("GET /api/v1/audit.csv", func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.authenticate(r); err != nil {
			fail(w, err)
			return
		}

		s.dyn.mu.RLock()
		logs := s.dyn.auditLogs
		s.dyn.mu.RUnlock()

		var buf bytes.Buffer
		// UTF-8 BOM
		buf.WriteString("\xef\xbb\xbf")
		writer := csv.NewWriter(&buf)
		_ = writer.Write([]string{"Waktu", "Jenis", "Keputusan", "Objek", "Catatan", "Oleh", "Peran"})

		for _, l := range logs {
			_ = writer.Write([]string{
				str(l["at"]),
				str(l["kind"]),
				str(l["title"]),
				str(l["detail"]),
				str(l["note"]),
				str(l["by"]),
				str(l["role"]),
			})
		}
		writer.Flush()

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=audit.csv")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	})

	// 17. Realtime SSE
	mux.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.authenticate(r); err != nil {
			fail(w, err)
			return
		}
		if s.Hub != nil {
			s.Hub.ServeHTTP(w, r)
			return
		}
		// If hub is not configured, fall back to SSE keep-alive
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, ": keep-alive\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	})
}
