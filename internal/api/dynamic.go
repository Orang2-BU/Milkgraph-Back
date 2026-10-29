package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"milkgraph/internal/domain"
	"milkgraph/internal/rules"
	"milkgraph/internal/store"
)

type DynamicState struct{}
type decisionBody struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

func noteValid(note string, required bool) error {
	if utf8.RuneCountInString(note) > 500 {
		return invalid("Catatan maksimal 500 karakter.", "note")
	}
	if required && strings.TrimSpace(note) == "" {
		return invalid("Catatan wajib diisi untuk penolakan.", "note")
	}
	return nil
}
func decodeOptional(r *http.Request, target any) error {
	if r.Body == nil {
		return nil
	}
	reader := bufio.NewReader(r.Body)
	for {
		b, e := reader.Peek(1)
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return &apiError{400, "BAD_REQUEST", "JSON tidak valid.", ""}
		}
		if !strings.ContainsRune(" \t\r\n", rune(b[0])) {
			break
		}
		_, _ = reader.ReadByte()
	}
	r.Body = io.NopCloser(reader)
	return decode(r, target)
}
func strictLimits(raw json.RawMessage) (rules.Limits, error) {
	var l rules.Limits
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil || keys == nil {
		return l, invalid("Limits harus berupa objek lengkap.", "limits")
	}
	for _, group := range limitGroups {
		for _, p := range group.Params {
			v, ok := keys[p.Key]
			if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
				return l, invalid("Semua batas constraint wajib diisi.", p.Key)
			}
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&l); err != nil {
		return l, &apiError{400, "BAD_REQUEST", "JSON tidak valid.", ""}
	}
	return l, validateLimits(l)
}
func (s *Server) requestLimits(r *http.Request) (rules.Limits, error) {
	l, err := s.limits(r.Context())
	if err != nil {
		return l, err
	}
	var body struct {
		Limits json.RawMessage `json:"limits"`
	}
	if err := decodeOptional(r, &body); err != nil {
		return l, err
	}
	if body.Limits != nil {
		return strictLimits(body.Limits)
	}
	return l, nil
}
func (s *Server) rulePlan(id string) (rules.Plan, error) {
	var p rules.Plan
	m := s.plan(id)
	if m == nil {
		return p, missing()
	}
	b, e := json.Marshal(m)
	if e != nil {
		return p, e
	}
	e = json.Unmarshal(b, &p)
	return p, e
}
func (s *Server) resetDemo(ctx context.Context) error {
	if err := s.dbRequired(); err != nil {
		return err
	}
	return s.DB.Reset(ctx)
}
func (s *Server) registerDynamic(register func(string, string, endpoint), mux *http.ServeMux) {
	register("POST /api/v1/session/role", "", func(r *http.Request) (any, error) {
		if s.Mode != "" && s.Mode != "demo" {
			return nil, &apiError{403, "FORBIDDEN", "Hanya tersedia dalam mode demo.", ""}
		}
		var b struct {
			Role string `json:"role"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		def, ok := domain.RolesMap[domain.RoleId(b.Role)]
		if !ok {
			return nil, invalid("Role tidak valid.", "role")
		}
		perms := domain.PermissionsForRole(domain.RoleId(b.Role))
		sort.Slice(perms, func(i, j int) bool { return perms[i] < perms[j] })
		return map[string]any{"result": domain.Session{User: domain.UserInfo{Name: def.User, Role: def.Label, Initials: def.Initials, Shift: "Shift Siang"}, Role: domain.RoleId(b.Role), Permissions: perms}, "log": nil}, nil
	})
	register("POST /api/v1/demo/reset", "", func(r *http.Request) (any, error) {
		if s.Mode != "" && s.Mode != "demo" {
			return nil, &apiError{403, "FORBIDDEN", "Hanya tersedia dalam mode demo.", ""}
		}
		if e := decodeOptional(r, &struct{}{}); e != nil {
			return nil, e
		}
		if e := s.resetDemo(r.Context()); e != nil {
			return nil, e
		}
		return map[string]any{"result": map[string]any{"ok": true}, "log": nil}, nil
	})
	register("GET /api/v1/notifications", "", func(r *http.Request) (any, error) {
		l, e := s.limits(r.Context())
		if e != nil {
			return nil, e
		}
		p, e := s.rulePlan("current")
		if e != nil {
			return nil, e
		}
		c := rules.EvaluateResult(p, l)
		var d domain.PlanDecision
		if e = s.state(r.Context(), "decision", "current", &d); e != nil {
			return nil, e
		}
		sub := fmt.Sprintf("%d constraint dilanggar · starvation %d menit", c.Violations.Before, c.Starvation.Before)
		tone := "crit"
		if d.Status == "approved" {
			sub = "Rekomendasi disetujui · RT-26 dialihkan ke Bay 1"
			tone = "ok"
		}
		return []map[string]any{
			{"id": "PLAN-FIFO", "title": "Rencana shift tidak layak", "subtitle": sub, "sourceId": "PLAN-FIFO", "tone": tone},
			{"id": "HOLD-RT26", "title": "RT-26 mendekati batas holding time", "subtitle": fmt.Sprintf("Tersisa %dj %02dm · menunggu bay", c.HoldingLeft/60, c.HoldingLeft%60), "sourceId": "HOLD-RT26", "tone": "crit"},
			{"id": "LAB-8812", "title": "QC FG out-of-spec FG-1013-07", "subtitle": "Residu antibiotik · jalankan trace", "sourceId": "LAB-8812", "tone": "crit"},
		}, nil
	})
	register("GET /api/v1/receiving", "", func(r *http.Request) (any, error) { return s.receiving(r.Context()) })
	register("POST /api/v1/plans/{id}/check", "", func(r *http.Request) (any, error) {
		p, e := s.rulePlan(r.PathValue("id"))
		if e != nil {
			return nil, e
		}
		l, e := s.requestLimits(r)
		if e != nil {
			return nil, e
		}
		return rules.EvaluateResult(p, l), nil
	})
	register("POST /api/v1/plans/{id}/solve", "plan.decide", func(r *http.Request) (any, error) {
		if err := s.dbRequired(); err != nil {
			return nil, err
		}
		id := r.PathValue("id")
		p, e := s.rulePlan(id)
		if e != nil {
			return nil, e
		}
		l, e := s.requestLimits(r)
		if e != nil {
			return nil, e
		}
		a, _ := s.authenticate(r)
		view := s.planView(s.plan(id))
		view["simulation"] = true
		view["check"] = rules.EvaluateResult(p, l)
		return s.change(r.Context(), "solution", id, func(tx *store.Tx, raw json.RawMessage) (store.Change, error) {
			return store.Change{Value: view, Log: auditEntry(a, "rencana", "Simulasi rekomendasi rencana", "Jadwal skenario tersimpan; bukan solver langsung.", "", "neutral"), Event: "plan.solved"}, nil
		})
	})
	register("GET /api/v1/decisions", "", func(r *http.Request) (any, error) {
		if s.DB == nil {
			return []any{}, nil
		}
		return s.DB.List(r.Context(), "decision")
	})
	register("POST /api/v1/plans/{id}/decision", "plan.decide", func(r *http.Request) (any, error) {
		if err := s.dbRequired(); err != nil {
			return nil, err
		}
		id := r.PathValue("id")
		p, e := s.rulePlan(id)
		if e != nil {
			return nil, e
		}
		var b decisionBody
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		if b.Status != "approved" && b.Status != "rejected" {
			return nil, invalid("Status harus approved atau rejected.", "status")
		}
		if e = noteValid(b.Note, b.Status == "rejected"); e != nil {
			return nil, e
		}
		a, _ := s.authenticate(r)
		return s.change(r.Context(), "decision", id, func(tx *store.Tx, raw json.RawMessage) (store.Change, error) {
			if len(raw) > 0 {
				return store.Change{}, conflict()
			}
			l := rules.DefaultLimits
			lr, e := tx.Get(r.Context(), "limits", "current")
			if e != nil {
				return store.Change{}, e
			}
			if len(lr) > 0 {
				if e = json.Unmarshal(lr, &l); e != nil {
					return store.Change{}, e
				}
			}
			if b.Status == "approved" && rules.EvaluateResult(p, l).Violations.After > 0 {
				return store.Change{}, invalid("Rekomendasi masih melanggar constraint.", "status")
			}
			title, tone := "Rekomendasi disetujui", "ok"
			if b.Status == "rejected" {
				title, tone = "Rekomendasi ditolak", "warn"
			}
			return store.Change{Value: domain.PlanDecision{Plan: id, Status: b.Status, Note: b.Note, By: a.Name, At: "14.42"}, Log: auditEntry(a, "rencana", title, str(s.plan(id)["label"]), b.Note, tone), Event: "plan.decided"}, nil
		})
	})
	register("GET /api/v1/limits", "", func(r *http.Request) (any, error) { return s.limits(r.Context()) })
	register("PUT /api/v1/limits", "constraint.edit", func(r *http.Request) (any, error) {
		if err := s.dbRequired(); err != nil {
			return nil, err
		}
		var raw json.RawMessage
		if e := decode(r, &raw); e != nil {
			return nil, e
		}
		l, e := strictLimits(raw)
		if e != nil {
			return nil, e
		}
		a, _ := s.authenticate(r)
		return s.change(r.Context(), "limits", "current", func(tx *store.Tx, raw json.RawMessage) (store.Change, error) {
			old := rules.DefaultLimits
			if len(raw) > 0 {
				if e := json.Unmarshal(raw, &old); e != nil {
					return store.Change{}, e
				}
			}
			return store.Change{Value: l, Log: auditEntry(a, "constraint", "Batas constraint diubah", formatLimitsDiff(old, l), "", "neutral"), Event: "limits.updated"}, nil
		})
	})
	register("GET /api/v1/plans/{id}/instructions", "", func(r *http.Request) (any, error) { return s.instructions(r.Context(), r.PathValue("id")) })
	register("POST /api/v1/instructions/{id}/ack", "instruction.ack", func(r *http.Request) (any, error) {
		if err := s.dbRequired(); err != nil {
			return nil, err
		}
		if e := decodeOptional(r, &struct{}{}); e != nil {
			return nil, e
		}
		id := r.PathValue("id")
		planID, _, ok := strings.Cut(id, ":")
		if !ok {
			return nil, missing()
		}
		p, e := s.rulePlan(planID)
		if e != nil {
			return nil, e
		}
		a, _ := s.authenticate(r)
		var target *rules.Instruction
		for _, ins := range rules.Instructions(p) {
			if ins.ID == id {
				x := ins
				target = &x
				break
			}
		}
		if target == nil {
			return nil, missing()
		}
		if target.Role != a.Role {
			return nil, &apiError{403, "FORBIDDEN", "Hanya " + roleLabel(target.Role) + " yang dapat melakukan ini.", ""}
		}
		var view domain.InstructionView
		result, e := s.change(r.Context(), "decision", planID, func(tx *store.Tx, raw json.RawMessage) (store.Change, error) {
			var d domain.PlanDecision
			if len(raw) == 0 {
				return store.Change{}, missing()
			}
			if e := json.Unmarshal(raw, &d); e != nil {
				return store.Change{}, e
			}
			if d.Status != "approved" {
				return store.Change{}, missing()
			}
			ack, e := tx.Get(r.Context(), "ack", id)
			if e != nil {
				return store.Change{}, e
			}
			if len(ack) > 0 {
				return store.Change{}, conflict()
			}
			view = domain.InstructionView{ID: target.ID, Role: target.Role, Text: target.Text, From: target.From, AckedBy: a.Name, AckedAt: "14.42"}
			if e = tx.Put(r.Context(), "ack", id, view); e != nil {
				return store.Change{}, e
			}
			return store.Change{Value: d, Log: auditEntry(a, "instruksi", "Instruksi dikonfirmasi", target.Text, "", "ok"), Event: "instruction.acked"}, nil
		})
		if e != nil {
			return nil, e
		}
		out := result.(map[string]any)
		out["result"] = view
		return out, nil
	})
	register("GET /api/v1/supply/demand-sends", "", func(r *http.Request) (any, error) {
		date := r.URL.Query().Get("date")
		if date == "" {
			date = "2026-10-14"
		}
		if date != "2026-10-14" {
			return domain.DemandSendRecord{Sent: false}, nil
		}
		var rec domain.DemandSendRecord
		e := s.state(r.Context(), "demand", date, &rec)
		return rec, e
	})
	register("POST /api/v1/supply/demand/send", "demand.send", func(r *http.Request) (any, error) {
		if err := s.dbRequired(); err != nil {
			return nil, err
		}
		if e := decodeOptional(r, &struct{}{}); e != nil {
			return nil, e
		}
		a, _ := s.authenticate(r)
		detail := s.demandDetail()
		return s.change(r.Context(), "demand", "2026-10-14", func(tx *store.Tx, raw json.RawMessage) (store.Change, error) {
			if len(raw) > 0 {
				return store.Change{}, conflict()
			}
			return store.Change{Value: domain.DemandSendRecord{Sent: true, At: "14.42", By: a.Name, Detail: detail}, Log: auditEntry(a, "rencana", "Kebutuhan susu dikirim ke farm", detail, "", "ok"), Event: "demand.sent"}, nil
		})
	})
	register("GET /api/v1/lab/results", "", func(r *http.Request) (any, error) {
		kind := r.URL.Query().Get("kind")
		if kind != "" && kind != "penerimaan" && kind != "qc-fg" {
			return nil, invalid("Jenis lab tidak valid.", "kind")
		}
		rows, e := s.labResults(r.Context())
		if e != nil {
			return nil, e
		}
		out := []any{}
		for _, m := range rows {
			if kind == "" || str(m["kind"]) == kind {
				out = append(out, m)
			}
		}
		return out, nil
	})
	register("GET /api/v1/lab/summary", "", func(r *http.Request) (any, error) {
		v, e := s.receiving(r.Context())
		if e != nil {
			return nil, e
		}
		rows, e := s.labResults(r.Context())
		if e != nil {
			return nil, e
		}
		released := 0
		for _, m := range rows {
			if str(m["kind"]) == "penerimaan" && (str(m["status"]) == "lulus" || str(obj(m["decision"])["status"]) == "dirilis") {
				released++
			}
		}
		summary := obj(v["labSummary"])
		return map[string]any{"waiting": summary["waiting"], "released": released, "avgWaitMin": summary["avgMinutes"]}, nil
	})
	register("POST /api/v1/lab/results/{id}/decision", "lab.decide", func(r *http.Request) (any, error) {
		if err := s.dbRequired(); err != nil {
			return nil, err
		}
		id := r.PathValue("id")
		lab := item(s.seeds.Ops["labResults"], id)
		if lab == nil {
			return nil, missing()
		}
		var b decisionBody
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		if b.Status != "dirilis" && b.Status != "ditolak" {
			return nil, invalid("Status harus dirilis atau ditolak.", "status")
		}
		if e := noteValid(b.Note, b.Status == "ditolak"); e != nil {
			return nil, e
		}
		if str(lab["kind"]) != "penerimaan" {
			return nil, invalid("Hanya hasil lab penerimaan yang dapat diputuskan.", "kind")
		}
		status := str(lab["status"])
		if status == "uji-ulang" && b.Status == "dirilis" {
			return nil, invalid("Rilis menunggu hasil uji ulang.", "status")
		}
		if status != "hasil-siap" && status != "uji-ulang" {
			return nil, invalid("Hasil lab belum siap atau sudah selesai.", "status")
		}
		a, _ := s.authenticate(r)
		return s.change(r.Context(), "lab", id, func(tx *store.Tx, raw json.RawMessage) (store.Change, error) {
			if len(raw) > 0 {
				return store.Change{}, conflict()
			}
			view := clone(lab)
			view["decision"] = domain.LabDecision{Status: b.Status, Note: b.Note, By: a.Name, At: "14.42"}
			tone := "ok"
			if b.Status == "ditolak" {
				tone = "crit"
			}
			return store.Change{Value: view, Log: auditEntry(a, "lab", fmt.Sprintf("Hasil lab %s %s", str(lab["subject"]), b.Status), id, b.Note, tone), Event: "lab.decided"}, nil
		})
	})
	s.registerHolds(register)
	register("GET /api/v1/data-sources", "", func(r *http.Request) (any, error) {
		out := []any{}
		for _, v := range arr(s.seeds.Ops["dataSources"]) {
			m := clone(obj(v))
			if e := s.state(r.Context(), "source", str(m["id"]), &m); e != nil {
				return nil, e
			}
			m["simulation"] = true
			out = append(out, m)
		}
		return out, nil
	})
	register("POST /api/v1/data-sources/{id}/test", "", func(r *http.Request) (any, error) {
		if item(s.seeds.Ops["dataSources"], r.PathValue("id")) == nil {
			return nil, missing()
		}
		if e := decodeOptional(r, &struct{}{}); e != nil {
			return nil, e
		}
		return map[string]any{"ok": false, "text": "Simulasi: koneksi langsung belum dikonfigurasi.", "latencyMs": 0, "simulation": true}, nil
	})
	register("POST /api/v1/data-sources/{id}/sync", "", func(r *http.Request) (any, error) {
		if err := s.dbRequired(); err != nil {
			return nil, err
		}
		id := r.PathValue("id")
		m := item(s.seeds.Ops["dataSources"], id)
		if m == nil {
			return nil, missing()
		}
		if e := decodeOptional(r, &struct{}{}); e != nil {
			return nil, e
		}
		m["lastSync"] = "14.42"
		m["simulation"] = true
		m["status"] = "simulasi"
		m["syncNote"] = "Snapshot simulasi; tidak ada koneksi langsung."
		return s.change(r.Context(), "source", id, func(tx *store.Tx, raw json.RawMessage) (store.Change, error) {
			return store.Change{Value: m, Event: "source.synced"}, nil
		})
	})
	register("GET /api/v1/site/status", "", func(r *http.Request) (any, error) {
		view, e := s.siteView(r)
		if e != nil {
			return nil, e
		}
		labs, err := s.labResults(r.Context())
		if err != nil {
			return nil, err
		}
		m := clone(obj(obj(s.seeds.Site["status"])[view]))
		m["viewLabel"] = "Rencana awal · 14.42"
		if view == "after" {
			m["viewLabel"] = "Rekomendasi · proyeksi 15.30"
		} else {
			tankerWait := map[string]int{}
			for _, t := range arr(s.seeds.Scenario["tankers"]) {
				tankerWait[str(obj(t)["id"])] = num(obj(t)["wait"])
			}
			waiting := 0
			totalWait := 0
			decisions := map[string]string{}
			for _, lab := range labs {
				subj := str(lab["subject"])
				if dec := obj(lab["decision"]); dec != nil && str(dec["status"]) != "" {
					decisions[subj] = str(dec["status"])
					continue
				}
				if str(lab["kind"]) == "penerimaan" {
					st := str(lab["status"])
					if st == "menunggu" || st == "uji-ulang" || st == "hasil-siap" {
						waiting++
						totalWait += tankerWait[subj]
					}
				}
			}
			if waiting == 0 {
				m["lab"] = map[string]any{
					"tone":  "ok",
					"label": "Lab test · 0 RT · 0 mnt",
				}
			} else if waiting != 3 {
				avg := int(math.Round(float64(totalWait) / float64(waiting)))
				tone := "crit"
				if avg <= 45 {
					tone = "warn"
				}
				m["lab"] = map[string]any{
					"tone":  tone,
					"label": fmt.Sprintf("Lab test · %d RT · %d mnt", waiting, avg),
				}
			}
			if len(decisions) > 0 {
				yard := []any{}
				for _, raw := range arr(m["yard"]) {
					rtId := str(obj(raw)["rt"])
					if decisions[rtId] == "ditolak" {
						continue
					}
					yard = append(yard, raw)
				}
				m["yard"] = yard
			}
		}
		return m, nil
	})
	register("GET /api/v1/site/bottlenecks", "", func(r *http.Request) (any, error) {
		view, e := s.siteView(r)
		if e != nil {
			return nil, e
		}
		l, err := s.limits(r.Context())
		if err != nil {
			return nil, err
		}
		labs, err := s.labResults(r.Context())
		if err != nil {
			return nil, err
		}
		bList := clone(arr(obj(s.seeds.Site["bottlenecks"])[view]))
		if view == "before" {
			tankerWait := map[string]int{}
			for _, t := range arr(s.seeds.Scenario["tankers"]) {
				tankerWait[str(obj(t)["id"])] = num(obj(t)["wait"])
			}
			waiting := 0
			totalWait := 0
			for _, lab := range labs {
				if dec := obj(lab["decision"]); dec != nil && str(dec["status"]) != "" {
					continue
				}
				if str(lab["kind"]) == "penerimaan" {
					st := str(lab["status"])
					if st == "menunggu" || st == "uji-ulang" || st == "hasil-siap" {
						waiting++
						totalWait += tankerWait[str(lab["subject"])]
					}
				}
			}
			hl := rules.HoldingLeft(l)
			hlStr := fmt.Sprintf("%dj %02dm", hl/60, hl%60)
			for i, v := range bList {
				bm := obj(v)
				if str(bm["focus"]) == "lab" {
					if waiting == 0 {
						bm["text"] = "Semua hasil lab telah dirilis."
						bm["tone"] = "ok"
					} else {
						avg := int(math.Round(float64(totalWait) / float64(waiting)))
						bm["text"] = fmt.Sprintf("%d RT menunggu hasil, rata-rata %d menit.", waiting, avg)
					}
					bList[i] = bm
				}
				if str(bm["focus"]) == "yard" {
					bm["text"] = fmt.Sprintf("Sudah rilis lab, holding time tersisa %s.", hlStr)
					bList[i] = bm
				}
			}
		}
		return bList, nil
	})
	register("GET /api/v1/audit", "", func(r *http.Request) (any, error) {
		kind, before, limit, e := auditQuery(r)
		if e != nil {
			return nil, e
		}
		if s.DB == nil {
			return s.auditFallback(kind, before, limit), nil
		}
		return s.DB.Audit(r.Context(), kind, before, limit)
	})
	mux.HandleFunc("GET /api/v1/audit.csv", s.auditCSV)
	mux.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		if _, e := s.authenticate(r); e != nil {
			fail(w, e)
			return
		}
		if s.Hub == nil {
			fail(w, &apiError{503, "UNAVAILABLE", "Realtime tidak tersedia.", ""})
			return
		}
		s.Hub.ServeHTTP(w, r)
	})
}

func (s *Server) siteView(r *http.Request) (string, error) {
	view := r.URL.Query().Get("view")
	if view != "" && view != "before" && view != "after" {
		return "", invalid("View tidak valid.", "view")
	}
	if view != "" {
		return view, nil
	}
	var d domain.PlanDecision
	if e := s.state(r.Context(), "decision", "current", &d); e != nil {
		return "", e
	}
	if d.Status == "approved" {
		return "after", nil
	}
	return "before", nil
}
func (s *Server) demandDetail() string {
	total := 0.0
	for _, x := range arr(s.seeds.Ops["demandPlan"]) {
		m := obj(x)
		total += math.Round(float64(num(m["packs"])) * floatValue(m["litresPerPack"]) * floatValue(m["milk"]))
	}
	total = math.Round(total / (1 - floatValue(s.seeds.Ops["PROCESS_LOSS"])))
	rt := 0
	for _, f := range arr(s.seeds.Ops["farms"]) {
		rt += num(obj(f)["plannedRt"])
	}
	return fmt.Sprintf("%s L untuk %d SKU, %d RT dari %d farm", formatThousands(int(total)), len(arr(s.seeds.Ops["demandPlan"])), rt, len(arr(s.seeds.Ops["farms"])))
}
func floatValue(v any) float64 { n, _ := v.(float64); return n }
func (s *Server) labResults(ctx context.Context) ([]map[string]any, error) {
	out := []map[string]any{}
	saved := map[string]map[string]any{}
	if s.DB != nil {
		rows, e := s.DB.List(ctx, "lab")
		if e != nil {
			return nil, e
		}
		for _, raw := range rows {
			var m map[string]any
			if e = json.Unmarshal(raw, &m); e != nil {
				return nil, e
			}
			saved[str(m["id"])] = m
		}
	}
	for _, v := range arr(s.seeds.Ops["labResults"]) {
		m := clone(obj(v))
		if x, ok := saved[str(m["id"])]; ok {
			m = x
		}
		if str(m["status"]) == "uji-ulang" {
			m["eta"] = "15.30"
		}
		out = append(out, m)
	}
	return out, nil
}
func (s *Server) receiving(ctx context.Context) (map[string]any, error) {
	l, e := s.limits(ctx)
	if e != nil {
		return nil, e
	}
	labs, e := s.labResults(ctx)
	if e != nil {
		return nil, e
	}
	bySubject := map[string]string{}
	for _, m := range labs {
		bySubject[str(m["subject"])] = str(obj(m["decision"])["status"])
	}
	tankers := clone(arr(s.seeds.Scenario["tankers"]))
	counts := map[string]int{}
	waiting, total := 0, 0
	for _, v := range tankers {
		m := obj(v)
		switch bySubject[str(m["id"])] {
		case "dirilis":
			m["stage"] = "Released"
			m["wait"] = 0
		case "ditolak":
			m["stage"] = "Ditolak"
			m["wait"] = 0
		}
		stage := str(m["stage"])
		counts[stage]++
		if stage == "Lab test" {
			waiting++
			total += num(m["wait"])
		}
	}
	avg := 0
	if waiting > 0 {
		avg = int(math.Round(float64(total) / float64(waiting)))
	}
	stages := clone(arr(s.seeds.Scenario["stages"]))
	for _, v := range stages {
		m := obj(v)
		switch str(m["label"]) {
		case "Sampling", "Lab test", "Released", "Unloading":
			m["count"] = counts[str(m["label"])]
		}
		if str(m["label"]) == "Lab test" {
			m["status"] = fmt.Sprintf("Tunggu %d menit", avg)
		}
	}
	return map[string]any{"metrics": clone(arr(s.seeds.Scenario["metrics"])), "stages": stages, "tankers": tankers, "assets": clone(arr(s.seeds.Scenario["assets"])), "labHistory": clone(arr(s.seeds.Scenario["labHistory"])), "labSummary": map[string]any{"avgMinutes": avg, "deltaMinutes": avg - 38, "waiting": waiting, "sourceId": "LAB-QUEUE"}, "incident": map[string]any{"planId": "current", "rt": "RT-26", "holdingLeftMin": rules.HoldingLeft(l), "summary": "Antrean RT menumpuk karena lab test lama dan Bay 2 sedang CIP.", "factors": []map[string]any{{"label": fmt.Sprintf("Lab test %d menit", avg), "sourceId": "LAB-QUEUE"}, {"label": "Bay 2 CIP sampai 15.20", "sourceId": "CIP-B2"}, {"label": "Holding time RT-26", "sourceId": "HOLD-RT26"}}}}, nil
}
func (s *Server) instructions(ctx context.Context, id string) ([]domain.InstructionView, error) {
	p, e := s.rulePlan(id)
	if e != nil {
		return nil, e
	}
	out := []domain.InstructionView{}
	if s.DB == nil {
		return out, nil
	}
	// A single statement keeps plan and acknowledgement reads consistent across reset.
	rows, e := s.DB.Pool.Query(ctx, "SELECT entity, body FROM milkgraph.state WHERE (entity='decision' AND id=$1) OR (entity='ack' AND split_part(id, ':', 1)=$1)", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var d domain.PlanDecision
	acks := map[string]domain.InstructionView{}
	for rows.Next() {
		var entity string
		var raw []byte
		if e = rows.Scan(&entity, &raw); e != nil {
			return nil, e
		}
		if entity == "decision" {
			if e = json.Unmarshal(raw, &d); e != nil {
				return nil, e
			}
		} else {
			var a domain.InstructionView
			if e = json.Unmarshal(raw, &a); e != nil {
				return nil, e
			}
			acks[a.ID] = a
		}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if d.Status != "approved" {
		return out, nil
	}
	for _, ins := range rules.Instructions(p) {
		v := domain.InstructionView{ID: ins.ID, Role: ins.Role, Text: ins.Text, From: ins.From}
		if a, ok := acks[ins.ID]; ok {
			v.AckedBy = a.AckedBy
			v.AckedAt = a.AckedAt
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *Server) historicalHolds() []domain.Hold {
	out := []domain.Hold{}
	for _, v := range arr(s.seeds.Ops["pastHolds"]) {
		raw, _ := json.Marshal(v)
		var h domain.Hold
		if json.Unmarshal(raw, &h) == nil {
			out = append(out, h)
		}
	}
	return out
}
func (s *Server) holds(ctx context.Context) ([]domain.Hold, error) {
	out := s.historicalHolds()
	e := s.state(ctx, "holds", "current", &out)
	return out, e
}
func (s *Server) registerHolds(register func(string, string, endpoint)) {
	register("GET /api/v1/holds", "", func(r *http.Request) (any, error) { return s.holds(r.Context()) })
	register("POST /api/v1/holds", "hold.submit", func(r *http.Request) (any, error) {
		if err := s.dbRequired(); err != nil {
			return nil, err
		}
		var b struct {
			Trigger string       `json:"trigger"`
			RT      string       `json:"rt"`
			Lots    []domain.Lot `json:"lots"`
			Note    string       `json:"note"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		if e := noteValid(b.Note, false); e != nil {
			return nil, e
		}
		if len(b.Lots) == 0 || len(b.Lots) > 100 {
			return nil, invalid("Pilih 1-100 lot terdampak.", "lots")
		}
		back, e := s.trace.Backward(b.Trigger)
		if e != nil {
			return nil, invalid("Pemicu trace tidak terdaftar.", "trigger")
		}
		if str(back["subjectRt"]) != b.RT {
			return nil, invalid("RT tidak sesuai bukti trace.", "rt")
		}
		forward, e := s.trace.Forward(b.RT, "2026-10-13")
		if e != nil {
			return nil, invalid("RT tidak sesuai bukti trace.", "rt")
		}
		trusted := map[string]domain.Lot{}
		for _, x := range arr(forward["lots"]) {
			raw, _ := json.Marshal(x)
			var lot domain.Lot
			if e = json.Unmarshal(raw, &lot); e != nil {
				return nil, e
			}
			trusted[lot.ID] = lot
		}
		seen := map[string]bool{}
		verified := make([]domain.Lot, 0, len(b.Lots))
		for _, lot := range b.Lots {
			canonical, ok := trusted[lot.ID]
			if !ok || seen[lot.ID] {
				return nil, invalid("Lot tidak sesuai bukti trace atau duplikat.", "lots")
			}
			seen[lot.ID] = true
			verified = append(verified, canonical)
		}
		b.Lots = verified
		a, _ := s.authenticate(r)
		var created domain.Hold
		result, e := s.change(r.Context(), "holds", "current", func(tx *store.Tx, raw json.RawMessage) (store.Change, error) {
			holds := s.historicalHolds()
			if len(raw) > 0 {
				if e := json.Unmarshal(raw, &holds); e != nil {
					return store.Change{}, e
				}
			}
			for _, h := range holds {
				if h.Trigger == b.Trigger && h.Status != "ditolak" {
					return store.Change{}, conflict()
				}
			}
			existing := map[string]bool{}
			for _, h := range holds {
				existing[h.ID] = true
			}
			id := ""
			for {
				n, e := tx.NextHold(r.Context())
				if e != nil {
					return store.Change{}, e
				}
				id = fmt.Sprintf("HOLD-%03d", n)
				if !existing[id] {
					break
				}
			}
			created = domain.Hold{ID: id, Trigger: b.Trigger, RT: b.RT, Lots: b.Lots, Note: b.Note, Status: "diajukan", By: a.Name, At: "14.42"}
			holds = append([]domain.Hold{created}, holds...)
			return store.Change{Value: holds, Log: auditEntry(a, "hold", "Daftar hold diajukan ke QA", fmt.Sprintf("%d lot dari %s, pemicu %s", len(b.Lots), b.RT, b.Trigger), b.Note, "warn"), Event: "hold.created"}, nil
		})
		if e != nil {
			return nil, e
		}
		out := result.(map[string]any)
		out["result"] = created
		return out, nil
	})
	register("POST /api/v1/holds/{id}/review", "hold.review", func(r *http.Request) (any, error) {
		if err := s.dbRequired(); err != nil {
			return nil, err
		}
		id := r.PathValue("id")
		for _, h := range s.historicalHolds() {
			if h.ID == id {
				return nil, invalid("Riwayat hold bersifat baca saja.", "id")
			}
		}
		var b decisionBody
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		if b.Status != "aktif" && b.Status != "ditolak" && b.Status != "dilepas" {
			return nil, invalid("Status review tidak valid.", "status")
		}
		if e := noteValid(b.Note, b.Status != "aktif"); e != nil {
			return nil, e
		}
		a, _ := s.authenticate(r)
		var reviewed domain.Hold
		result, e := s.change(r.Context(), "holds", "current", func(tx *store.Tx, raw json.RawMessage) (store.Change, error) {
			if len(raw) == 0 {
				return store.Change{}, missing()
			}
			var holds []domain.Hold
			if e := json.Unmarshal(raw, &holds); e != nil {
				return store.Change{}, e
			}
			found := false
			for i, h := range holds {
				if h.ID != id {
					continue
				}
				found = true
				if !(h.Status == "diajukan" && (b.Status == "aktif" || b.Status == "ditolak") || h.Status == "aktif" && b.Status == "dilepas") {
					return store.Change{}, invalid("Transisi status hold tidak valid.", "status")
				}
				h.Status = b.Status
				h.Review = &domain.HoldReview{Note: b.Note, By: a.Name, At: "14.42"}
				holds[i] = h
				reviewed = h
			}
			if !found {
				return store.Change{}, missing()
			}
			title, tone := "Hold disetujui QA", "crit"
			if b.Status == "ditolak" {
				title, tone = "Pengajuan hold ditolak", "neutral"
			}
			if b.Status == "dilepas" {
				title, tone = "Hold dilepas", "ok"
			}
			return store.Change{Value: holds, Log: auditEntry(a, "hold", title, id, b.Note, tone), Event: "hold.reviewed"}, nil
		})
		if e != nil {
			return nil, e
		}
		out := result.(map[string]any)
		out["result"] = reviewed
		return out, nil
	})
	register("GET /api/v1/holds/{id}/genealogy", "", func(r *http.Request) (any, error) {
		holds, e := s.holds(r.Context())
		if e != nil {
			return nil, e
		}
		var h *domain.Hold
		for _, x := range holds {
			if x.ID == r.PathValue("id") {
				copy := x
				h = &copy
				break
			}
		}
		if h == nil {
			return nil, missing()
		}
		handoffs := any([]any{})
		if v, e := s.trace.Handoffs(h.Trigger); e == nil {
			handoffs = v
		}
		lots := []map[string]any{}
		ids := map[string]bool{}
		for _, l := range h.Lots {
			ids[l.ID] = true
			m := map[string]any{"id": l.ID, "product": l.Product, "percent": l.Percent, "packs": l.Packs, "place": "Tidak tercatat", "shipped": false}
			if loc, ok := obj(s.seeds.Ops["lotLocation"])[l.ID]; ok {
				m["place"] = obj(loc)["place"]
				m["shipped"] = obj(loc)["shipped"]
			}
			lots = append(lots, m)
		}
		tests := []map[string]any{}
		labRows, _ := s.labResults(r.Context())
		for _, m := range labRows {
			if str(m["subject"]) == h.RT || ids[str(m["subject"])] {
				resStatus := str(m["status"])
				if dec := obj(m["decision"]); dec != nil && str(dec["status"]) != "" {
					resStatus = str(dec["status"])
				}
				tests = append(tests, map[string]any{"id": m["id"], "subject": m["subject"], "result": resStatus})
			}
		}
		return map[string]any{"hold": h, "handoffs": handoffs, "labTests": tests, "lots": lots}, nil
	})
}
func auditQuery(r *http.Request) (string, int64, int, error) {
	q := r.URL.Query()
	kind := q.Get("kind")
	switch kind {
	case "", "rencana", "hold", "lab", "constraint", "instruksi":
	default:
		return "", 0, 0, invalid("Jenis audit tidak valid.", "kind")
	}
	before := int64(0)
	limit := 100
	var e error
	if v, ok := q["before"]; ok {
		if len(v) != 1 {
			return "", 0, 0, invalid("Cursor tidak valid.", "before")
		}
		before, e = strconv.ParseInt(v[0], 10, 64)
		if e != nil || before < 1 {
			return "", 0, 0, invalid("Cursor tidak valid.", "before")
		}
	}
	if v, ok := q["limit"]; ok {
		if len(v) != 1 {
			return "", 0, 0, invalid("Limit tidak valid.", "limit")
		}
		limit, e = strconv.Atoi(v[0])
		if e != nil || limit < 1 || limit > 1000 {
			return "", 0, 0, invalid("Limit harus 1-1000.", "limit")
		}
	}
	return kind, before, limit, nil
}
func (s *Server) auditFallback(kind string, before int64, limit int) []json.RawMessage {
	out := []json.RawMessage{}
	for _, x := range arr(s.seeds.Ops["pastLog"]) {
		m := obj(x)
		if (kind == "" || str(m["kind"]) == kind) && (before == 0 || int64(num(m["id"])) < before) {
			raw, _ := json.Marshal(m)
			out = append(out, raw)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		var a, b struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(out[i], &a)
		_ = json.Unmarshal(out[j], &b)
		return a.ID > b.ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
func csvSafe(s string) string {
	trimmed := strings.TrimLeft(s, " \t\r\n")
	if len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0])) || strings.HasPrefix(s, "\t") || strings.HasPrefix(s, "\r") || strings.HasPrefix(s, "\n") {
		return "'" + s
	}
	return s
}
func (s *Server) auditCSV(w http.ResponseWriter, r *http.Request) {
	if _, e := s.authenticate(r); e != nil {
		fail(w, e)
		return
	}
	kind, before, _, e := auditQuery(r)
	if e != nil {
		fail(w, e)
		return
	}
	page := func(before int64) ([]json.RawMessage, error) {
		if s.DB == nil {
			return s.auditFallback(kind, before, 1000), nil
		}
		return s.DB.Audit(r.Context(), kind, before, 1000)
	}
	rows, e := page(before)
	if e != nil {
		fail(w, e)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=audit.csv")
	if _, e = io.WriteString(w, "\xef\xbb\xbf"); e != nil {
		return
	}
	writer := csv.NewWriter(w)
	if e = writer.Write([]string{"Waktu", "Jenis", "Keputusan", "Objek", "Catatan", "Oleh", "Peran"}); e != nil {
		return
	}
	for {
		for _, raw := range rows {
			var m map[string]any
			if e = json.Unmarshal(raw, &m); e != nil {
				return
			}
			cells := []string{}
			for _, key := range []string{"at", "kind", "title", "detail", "note", "by", "role"} {
				cells = append(cells, csvSafe(str(m[key])))
			}
			if e = writer.Write(cells); e != nil {
				return
			}
			before = int64(num(m["id"]))
		}
		writer.Flush()
		if writer.Error() != nil || len(rows) < 1000 {
			return
		}
		rows, e = page(before)
		if e != nil {
			return
		}
	}
}
