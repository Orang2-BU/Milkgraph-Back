package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"milkgraph/internal/api"
)

type TestClient struct {
	server  *api.Server
	handler http.Handler
	role    string
}

func newClient(role string) *TestClient {
	s := &api.Server{Mode: "demo"}
	return &TestClient{
		server:  s,
		handler: s.Handler(),
		role:    role,
	}
}

func (c *TestClient) do(method, path string, body any) (*http.Response, map[string]any, error) {
	var bodyReader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, "/api/v1"+path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.role != "" {
		req.Header.Set("X-Demo-Role", c.role)
	}
	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, req)

	res := rec.Result()
	var resBody map[string]any
	_ = json.NewDecoder(res.Body).Decode(&resBody)
	return res, resBody, nil
}

func (c *TestClient) doList(method, path string, body any) (*http.Response, []any, error) {
	var bodyReader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, "/api/v1"+path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.role != "" {
		req.Header.Set("X-Demo-Role", c.role)
	}
	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, req)

	res := rec.Result()
	var resList []any
	_ = json.NewDecoder(res.Body).Decode(&resList)
	return res, resList, nil
}

// Test 16-Step Demo Scenario from 05-INTEGRASI-FE-BE.md §7
func TestSixteenStepDemoScenario(t *testing.T) {
	client := newClient("planner")

	// Step 0: Reset demo state
	res, _, _ := client.do("POST", "/demo/reset", nil)
	if res.StatusCode != 200 {
		t.Fatalf("reset failed: %d", res.StatusCode)
	}

	// Step 1: Planner opens /receiving
	res, recView, _ := client.do("GET", "/receiving", nil)
	if res.StatusCode != 200 {
		t.Fatalf("step 1 failed: %d", res.StatusCode)
	}
	incident, _ := recView["incident"].(map[string]any)
	if incident["rt"] != "RT-26" || int(incident["holdingLeftMin"].(float64)) != 70 {
		t.Fatalf("step 1 incident invalid: %v", incident)
	}

	// Step 2: Planner opens /rencana, checks violations
	res, checkRes, _ := client.do("POST", "/plans/current/check", nil)
	if res.StatusCode != 200 {
		t.Fatalf("step 2 check failed: %d", res.StatusCode)
	}
	violations, _ := checkRes["violations"].(map[string]any)
	if int(violations["before"].(float64)) != 3 || int(violations["after"].(float64)) != 0 {
		t.Fatalf("step 2 violations mismatch: %v", violations)
	}

	// Step 3: Planner approves plan
	res, decResp, _ := client.do("POST", "/plans/current/decision", map[string]any{
		"status": "approved",
		"note":   "Disetujui untuk menghindari starvation F2/F4",
	})
	if res.StatusCode != 200 {
		t.Fatalf("step 3 decision failed: %d", res.StatusCode)
	}
	decResult, _ := decResp["result"].(map[string]any)
	if decResult["status"] != "approved" {
		t.Fatalf("step 3 decision status mismatch: %v", decResult)
	}

	// Step 4: Planner opens /tangki to see instructions
	res, insList, _ := client.doList("GET", "/plans/current/instructions", nil)
	if res.StatusCode != 200 || len(insList) != 6 {
		t.Fatalf("step 4 instructions mismatch: len=%d, status=%d", len(insList), res.StatusCode)
	}

	// Step 5: Receiving coordinator confirms RT-26 instruction
	receivingClient := newClient("receiving")
	res, ackResp, _ := receivingClient.do("POST", "/instructions/current:RT-26/ack", nil)
	if res.StatusCode != 200 {
		t.Fatalf("step 5 ack failed: %d", res.StatusCode)
	}
	ackResult, _ := ackResp["result"].(map[string]any)
	if ackResult["ackedBy"] != "Dimas Prasetyo" {
		t.Fatalf("step 5 ackedBy mismatch: %v", ackResult)
	}

	// Step 6: Supervisor confirms F2 instruction
	spvClient := newClient("supervisor")
	res, spvAck, _ := spvClient.do("POST", "/instructions/current:F2/ack", nil)
	if res.StatusCode != 200 {
		t.Fatalf("step 6 spv ack failed: %d", res.StatusCode)
	}
	spvResult, _ := spvAck["result"].(map[string]any)
	if spvResult["ackedBy"] != "Hendra Kusuma" {
		t.Fatalf("step 6 spv ackedBy mismatch: %v", spvResult)
	}

	// Step 7: QA checks trace forward
	qaClient := newClient("qa")
	res, fwdResp, _ := qaClient.do("GET", "/trace/forward?rtId=RT-04&date=2026-10-13", nil)
	if res.StatusCode != 200 {
		t.Fatalf("step 7 trace forward failed: %d", res.StatusCode)
	}
	lots, _ := fwdResp["lots"].([]any)
	if len(lots) != 5 {
		t.Fatalf("step 7 lots count mismatch: %d", len(lots))
	}

	// Step 8: QA submits hold
	res, holdResp, _ := qaClient.do("POST", "/holds", map[string]any{
		"trigger": "FG-1013-07",
		"rt":      "RT-04",
		"lots": []map[string]any{
			{"id": "FG-1013-05", "product": "UHT Cokelat 200 ml", "percent": 18, "packs": 8200},
			{"id": "FG-1013-07", "product": "UHT Cokelat 200 ml", "percent": 12, "packs": 7900},
		},
		"note": "Residu antibiotik pada FG-1013-07",
	})
	if res.StatusCode != 201 {
		t.Fatalf("step 8 hold submit failed: %d", res.StatusCode)
	}
	holdResult, _ := holdResp["result"].(map[string]any)
	holdId := holdResult["id"].(string)

	// Step 9: QA approves hold review
	res, reviewResp, _ := qaClient.do("POST", "/holds/"+holdId+"/review", map[string]any{
		"status": "aktif",
		"note":   "Karantina 5 lot terdampak",
	})
	if res.StatusCode != 200 {
		t.Fatalf("step 9 review hold failed: %d", res.StatusCode)
	}
	revResult, _ := reviewResp["result"].(map[string]any)
	if revResult["status"] != "aktif" {
		t.Fatalf("step 9 hold status mismatch: %v", revResult)
	}

	// Step 10: QA decides lab result LAB-9031 (RT-27) release
	res, labResp, _ := qaClient.do("POST", "/lab/results/LAB-9031/decision", map[string]any{
		"status": "dirilis",
		"note":   "Parameter memenuhi spesifikasi SNI",
	})
	if res.StatusCode != 200 {
		t.Fatalf("step 10 lab decide failed: %d", res.StatusCode)
	}

	// Step 11: QA tries to release uji-ulang (LAB-9029), should be rejected 422
	res, _, _ = qaClient.do("POST", "/lab/results/LAB-9029/decision", map[string]any{
		"status": "dirilis",
		"note":   "Coba rilis",
	})
	if res.StatusCode != 422 {
		t.Fatalf("step 11 expected 422, got: %d", res.StatusCode)
	}

	// Step 12: Planner updates limits (holdingMin 630 -> 620)
	res, limitResp, _ := client.do("PUT", "/limits", map[string]any{
		"maxTransfer":     9000,
		"starvationTol":   20,
		"maintenanceBay3": false,
		"holdingMin":      620,
		"bufferMaxMin":    1080,
		"cipBuffer":       0,
		"breakdownF1":     false,
		"tankMax":         95,
		"changeoverMax":   2,
	})
	if res.StatusCode != 200 {
		t.Fatalf("step 12 put limits failed: %d", res.StatusCode)
	}
	limResult, _ := limitResp["result"].(map[string]any)
	if int(limResult["holdingMin"].(float64)) != 620 {
		t.Fatalf("step 12 limit update mismatch: %v", limResult)
	}

	// Step 13: Planner sends demand plan
	res, demandResp, _ := client.do("POST", "/supply/demand/send", nil)
	if res.StatusCode != 200 {
		t.Fatalf("step 13 demand send failed: %d", res.StatusCode)
	}
	demandResult, _ := demandResp["result"].(map[string]any)
	if demandResult["sent"] != true {
		t.Fatalf("step 13 demand sent status mismatch: %v", demandResult)
	}

	// Step 14: Check audit log
	res, auditList, _ := client.doList("GET", "/audit", nil)
	if res.StatusCode != 200 || len(auditList) < 10 {
		t.Fatalf("step 14 audit log list failed: len=%d, status=%d", len(auditList), res.StatusCode)
	}

	// Step 15: Check site status simulation
	res, siteStatus, _ := client.do("GET", "/site/status?view=after", nil)
	if res.StatusCode != 200 || siteStatus["viewLabel"] != "Rekomendasi · proyeksi 15.30" {
		t.Fatalf("step 15 site status mismatch: %v", siteStatus)
	}

	t.Log("All 16-step demo scenarios successfully verified!")
}
