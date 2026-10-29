package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"milkgraph/internal/domain"
	"milkgraph/internal/rules"
	"milkgraph/internal/store"
)

func testDB(t *testing.T) *store.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("MILKGRAPH_TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to isolated PostgreSQL")
	}
	db, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func durableRequest(h http.Handler, method, path, role, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, "/api/v1"+path, nil)
	}
	r.Header.Set("X-Demo-Role", role)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decodeMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decodeMap error %v on body %s", err, w.Body.String())
	}
	return m
}

func TestDurableMutationsRequireDB(t *testing.T) {
	h := (&Server{Mode: "demo"}).Handler()
	endpoints := []struct {
		method string
		path   string
		role   string
		body   string
	}{
		{"POST", "/plans/current/decision", "planner", `{"status":"approved","note":""}`},
		{"PUT", "/limits", "planner", `{}`},
		{"POST", "/instructions/current:RT-26/ack", "receiving", `{}`},
		{"POST", "/supply/demand/send", "planner", `{}`},
		{"POST", "/lab/results/LAB-9031/decision", "qa", `{"status":"dirilis","note":""}`},
		{"POST", "/holds", "qa", `{"trigger":"FG-1013-07","rt":"RT-04","lots":[]}`},
		{"POST", "/data-sources/scada/sync", "planner", `{}`},
		{"POST", "/demo/reset", "planner", `{}`},
	}

	for _, ep := range endpoints {
		w := durableRequest(h, ep.method, ep.path, ep.role, ep.body)
		if w.Code != 503 {
			t.Fatalf("expected 503 for %s %s without DB, got %d: %s", ep.method, ep.path, w.Code, w.Body.String())
		}
	}
}

func TestBootstrapAndRestartDurability(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	_ = db.Reset(ctx)

	s1 := &Server{Mode: "demo", DB: db}
	h1 := s1.Handler()

	if err := s1.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap 1 failed: %v", err)
	}
	if err := s1.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap 2 failed: %v", err)
	}

	// Update limits in s1
	newLimitsJSON := `{
		"maxTransfer": 9500,
		"starvationTol": 25,
		"maintenanceBay3": true,
		"holdingMin": 600,
		"bufferMaxMin": 1020,
		"cipBuffer": 20,
		"breakdownF1": false,
		"tankMax": 90,
		"changeoverMax": 3
	}`
	w := durableRequest(h1, "PUT", "/limits", "planner", newLimitsJSON)
	if w.Code != 200 {
		t.Fatalf("PUT /limits failed: %d %s", w.Code, w.Body.String())
	}

	// "Restart" server with fresh in-memory struct pointing to same DB
	s2 := &Server{Mode: "demo", DB: db}
	h2 := s2.Handler()

	w = durableRequest(h2, "GET", "/limits", "planner", "")
	if w.Code != 200 {
		t.Fatalf("GET /limits failed: %d %s", w.Code, w.Body.String())
	}
	var lim rules.Limits
	if err := json.Unmarshal(w.Body.Bytes(), &lim); err != nil {
		t.Fatal(err)
	}
	if lim.MaxTransfer != 9500 || !lim.MaintenanceBay3 || lim.HoldingMin != 600 {
		t.Fatalf("limits not persisted across restart: %+v", lim)
	}
}

func TestDurablePlanDecisionAndInstructionAck(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	_ = db.Reset(ctx)

	s := &Server{Mode: "demo", DB: db}
	h := s.Handler()

	// Ack before approval -> 404
	w := durableRequest(h, "POST", "/instructions/current:RT-26/ack", "receiving", `{}`)
	if w.Code != 404 {
		t.Fatalf("ack before approval want 404, got %d: %s", w.Code, w.Body.String())
	}

	// Instructions before approval -> empty
	w = durableRequest(h, "GET", "/plans/current/instructions", "planner", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("instructions before approval want [], got %d: %s", w.Code, w.Body.String())
	}

	// Approve plan
	w = durableRequest(h, "POST", "/plans/current/decision", "planner", `{"status":"approved","note":"Disetujui shift siang"}`)
	if w.Code != 200 {
		t.Fatalf("approve plan failed: %d %s", w.Code, w.Body.String())
	}

	// Duplicate decision -> 409
	w = durableRequest(h, "POST", "/plans/current/decision", "planner", `{"status":"approved","note":"Duplikat"}`)
	if w.Code != 409 {
		t.Fatalf("duplicate decision want 409, got %d: %s", w.Code, w.Body.String())
	}

	// Instructions after approval -> 6 instructions
	w = durableRequest(h, "GET", "/plans/current/instructions", "planner", "")
	if w.Code != 200 {
		t.Fatalf("instructions after approval failed: %d", w.Code)
	}
	var insList []domain.InstructionView
	if err := json.Unmarshal(w.Body.Bytes(), &insList); err != nil {
		t.Fatal(err)
	}
	if len(insList) != 6 {
		t.Fatalf("expected 6 instructions, got %d", len(insList))
	}

	// Receiving acks RT-26
	w = durableRequest(h, "POST", "/instructions/current:RT-26/ack", "receiving", `{}`)
	if w.Code != 200 {
		t.Fatalf("ack RT-26 failed: %d %s", w.Code, w.Body.String())
	}
	resp := decodeMap(t, w)
	resObj, _ := resp["result"].(map[string]any)
	if resObj["ackedBy"] != "Dimas Prasetyo" {
		t.Fatalf("ackedBy want Dimas Prasetyo, got %v", resObj["ackedBy"])
	}

	// Duplicate ack -> 409
	w = durableRequest(h, "POST", "/instructions/current:RT-26/ack", "receiving", `{}`)
	if w.Code != 409 {
		t.Fatalf("duplicate ack want 409, got %d: %s", w.Code, w.Body.String())
	}

	// Wrong role: receiving trying to ack supervisor instruction current:F2 -> 403
	w = durableRequest(h, "POST", "/instructions/current:F2/ack", "receiving", `{}`)
	if w.Code != 403 {
		t.Fatalf("wrong role ack want 403, got %d: %s", w.Code, w.Body.String())
	}

	// Supervisor acks current:F2 -> 200
	w = durableRequest(h, "POST", "/instructions/current:F2/ack", "supervisor", `{}`)
	if w.Code != 200 {
		t.Fatalf("supervisor ack failed: %d %s", w.Code, w.Body.String())
	}
}

func TestDurableLabDecisionAndReceiving(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	_ = db.Reset(ctx)

	s := &Server{Mode: "demo", DB: db}
	h := s.Handler()

	// Initial receiving
	w := durableRequest(h, "GET", "/receiving", "receiving", "")
	if w.Code != 200 {
		t.Fatalf("GET /receiving failed: %d", w.Code)
	}
	rec1 := decodeMap(t, w)
	stages1, _ := rec1["stages"].([]any)
	var labStage1 map[string]any
	for _, st := range stages1 {
		m := st.(map[string]any)
		if m["label"] == "Lab test" {
			labStage1 = m
		}
	}
	if labStage1 == nil || int(labStage1["count"].(float64)) != 3 {
		t.Fatalf("initial Lab test count want 3, got %v", labStage1)
	}

	// Decide lab result LAB-9031 (RT-27) as dirilis
	w = durableRequest(h, "POST", "/lab/results/LAB-9031/decision", "qa", `{"status":"dirilis","note":""}`)
	if w.Code != 200 {
		t.Fatalf("POST /lab/results/LAB-9031/decision failed: %d %s", w.Code, w.Body.String())
	}

	// Duplicate decision -> 409
	w = durableRequest(h, "POST", "/lab/results/LAB-9031/decision", "qa", `{"status":"dirilis","note":""}`)
	if w.Code != 409 {
		t.Fatalf("duplicate lab decision want 409, got %d: %s", w.Code, w.Body.String())
	}

	// Updated receiving
	w = durableRequest(h, "GET", "/receiving", "receiving", "")
	if w.Code != 200 {
		t.Fatalf("GET /receiving failed: %d", w.Code)
	}
	rec2 := decodeMap(t, w)
	tankers2, _ := rec2["tankers"].([]any)
	var rt27 map[string]any
	for _, tk := range tankers2 {
		m := tk.(map[string]any)
		if m["id"] == "RT-27" {
			rt27 = m
		}
	}
	if rt27 == nil || rt27["stage"] != "Released" {
		t.Fatalf("RT-27 stage want Released, got %v", rt27)
	}

	// Lab summary
	w = durableRequest(h, "GET", "/lab/summary", "qa", "")
	if w.Code != 200 {
		t.Fatalf("GET /lab/summary failed: %d", w.Code)
	}
	sum := decodeMap(t, w)
	if int(sum["released"].(float64)) < 4 { // initial 3 lulus + 1 newly released = 4
		t.Fatalf("released want >= 4, got %v", sum["released"])
	}
}

func TestDurableHoldSubmitAndReview(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	_ = db.Reset(ctx)

	s := &Server{Mode: "demo", DB: db}
	h := s.Handler()

	// Forward trace lots
	holdBody := `{
		"trigger": "FG-1013-07",
		"rt": "RT-04",
		"lots": [
			{"id": "FG-1013-05", "product": "UHT Cokelat 200 ml", "percent": 18, "packs": 8200},
			{"id": "FG-1013-06", "product": "UHT Cokelat 200 ml", "percent": 24, "packs": 8400}
		],
		"note": "Hold diajukan karena residu antibiotik"
	}`

	w := durableRequest(h, "POST", "/holds", "qa", holdBody)
	if w.Code != 201 {
		t.Fatalf("POST /holds want 201, got %d: %s", w.Code, w.Body.String())
	}
	holdResp := decodeMap(t, w)
	resultObj, _ := holdResp["result"].(map[string]any)
	holdID, _ := resultObj["id"].(string)
	if !strings.HasPrefix(holdID, "HOLD-") {
		t.Fatalf("hold ID format want HOLD-..., got %s", holdID)
	}

	// Duplicate submit same trigger -> 409
	w = durableRequest(h, "POST", "/holds", "qa", holdBody)
	if w.Code != 409 {
		t.Fatalf("duplicate hold submit want 409, got %d: %s", w.Code, w.Body.String())
	}

	// Review hold as aktif
	w = durableRequest(h, "POST", "/holds/"+holdID+"/review", "qa", `{"status":"aktif","note":""}`)
	if w.Code != 200 {
		t.Fatalf("review hold aktif failed: %d %s", w.Code, w.Body.String())
	}

	// Review hold as dilepas
	w = durableRequest(h, "POST", "/holds/"+holdID+"/review", "qa", `{"status":"dilepas","note":"Hasil uji ulang negatif"}`)
	if w.Code != 200 {
		t.Fatalf("review hold dilepas failed: %d %s", w.Code, w.Body.String())
	}

	// Reviewing historical read-only hold -> 422
	w = durableRequest(h, "POST", "/holds/HOLD-2610/review", "qa", `{"status":"aktif","note":""}`)
	if w.Code != 422 {
		t.Fatalf("review historical hold want 422, got %d: %s", w.Code, w.Body.String())
	}

	// Genealogy
	w = durableRequest(h, "GET", "/holds/"+holdID+"/genealogy", "qa", "")
	if w.Code != 200 {
		t.Fatalf("genealogy failed: %d %s", w.Code, w.Body.String())
	}
	gen := decodeMap(t, w)
	handoffs, _ := gen["handoffs"].([]any)
	if len(handoffs) != 7 {
		t.Fatalf("genealogy handoffs want 7, got %d", len(handoffs))
	}
}

func TestDurableDemandSend(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	_ = db.Reset(ctx)

	s := &Server{Mode: "demo", DB: db}
	h := s.Handler()

	w := durableRequest(h, "POST", "/supply/demand/send", "planner", `{}`)
	if w.Code != 200 {
		t.Fatalf("POST /supply/demand/send failed: %d %s", w.Code, w.Body.String())
	}
	resp := decodeMap(t, w)
	resObj, _ := resp["result"].(map[string]any)
	detail, _ := resObj["detail"].(string)
	if !strings.Contains(detail, "278.659 L") || !strings.Contains(detail, "6 SKU") {
		t.Fatalf("unexpected demand send detail: %s", detail)
	}

	// Duplicate send -> 409
	w = durableRequest(h, "POST", "/supply/demand/send", "planner", `{}`)
	if w.Code != 409 {
		t.Fatalf("duplicate demand send want 409, got %d: %s", w.Code, w.Body.String())
	}

	// GET demand-sends
	w = durableRequest(h, "GET", "/supply/demand-sends?date=2026-10-14", "planner", "")
	if w.Code != 200 {
		t.Fatalf("GET /supply/demand-sends failed: %d %s", w.Code, w.Body.String())
	}
	sendRec := decodeMap(t, w)
	if sendRec["sent"] != true {
		t.Fatalf("sent record want true, got %v", sendRec["sent"])
	}
}

func TestDurableAuditCSVAndFormulaEscaping(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	_ = db.Reset(ctx)

	s := &Server{Mode: "demo", DB: db}
	h := s.Handler()

	// Perform action with potential CSV injection in note
	w := durableRequest(h, "POST", "/plans/current/decision", "planner", `{"status":"approved","note":"=cmd|' /C calc'!A0"}`)
	if w.Code != 200 {
		t.Fatalf("decision with injection failed: %d %s", w.Code, w.Body.String())
	}

	// Request CSV
	r := httptest.NewRequest("GET", "/api/v1/audit.csv", nil)
	r.Header.Set("X-Demo-Role", "planner")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != 200 {
		t.Fatalf("GET /audit.csv want 200, got %d", rec.Code)
	}
	csvBody := rec.Body.String()
	if !strings.Contains(csvBody, "'=cmd|' /C calc'!A0") {
		t.Fatalf("formula escaping failed, got:\n%s", csvBody)
	}
}
