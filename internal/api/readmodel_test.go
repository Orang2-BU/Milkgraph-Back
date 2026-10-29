package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http/httptest"

	"milkgraph/internal/store"
	"strings"
	"testing"
	"time"

	"milkgraph/internal/rules"
)

func TestSiteTracksLabDecisionsAndLimits(t *testing.T) {
	db := testDB(t)
	if err := db.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := (&Server{Mode: "demo", DB: db}).Handler()
	for _, tc := range []struct{ id, status, note string }{
		{"LAB-9031", "dirilis", ""}, {"LAB-9032", "dirilis", ""}, {"LAB-9029", "ditolak", "Uji ulang tidak lolos"},
	} {
		body, _ := json.Marshal(map[string]string{"status": tc.status, "note": tc.note})
		w := durableRequest(h, "POST", "/lab/results/"+tc.id+"/decision", "qa", string(body))
		if w.Code != 200 {
			t.Fatalf("lab decision: %d %s", w.Code, w.Body)
		}
		if tc.id == "LAB-9031" {
			w = durableRequest(h, "GET", "/site/status?view=before", "qa", "")
			if w.Code != 200 || obj(decodeMap(t, w)["lab"])["label"] != "Lab test · 2 RT · 54 mnt" {
				t.Fatalf("site waiting label stale: %s", w.Body)
			}
		}
	}
	w := durableRequest(h, "GET", "/site/status?view=before", "qa", "")
	m := decodeMap(t, w)
	if obj(m["lab"])["label"] != "Lab test · 0 RT · 0 mnt" || obj(m["lab"])["tone"] != "ok" {
		t.Fatalf("empty queue stale: %v", m["lab"])
	}
	for _, raw := range arr(m["yard"]) {
		if str(obj(raw)["rt"]) == "RT-24" {
			t.Fatal("rejected RT remains in yard")
		}
	}
	l := rules.DefaultLimits
	l.HoldingMin = 620
	b, _ := json.Marshal(l)
	if w = durableRequest(h, "PUT", "/limits", "planner", string(b)); w.Code != 200 {
		t.Fatalf("limits: %d %s", w.Code, w.Body)
	}
	w = durableRequest(h, "GET", "/site/bottlenecks?view=before", "qa", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "1j 00m") || strings.Contains(w.Body.String(), "3 RT menunggu") {
		t.Fatalf("bottlenecks stale: %s", w.Body)
	}
}

func TestSiteReadPropagatesDatabaseFailure(t *testing.T) {
	db := testDB(t)
	db.Close()
	h := (&Server{Mode: "demo", DB: db}).Handler()
	for _, path := range []string{"/site/status?view=before", "/site/bottlenecks?view=before"} {
		w := durableRequest(h, "GET", path, "qa", "")
		if w.Code == 200 {
			t.Fatalf("%s masked database failure: %s", path, w.Body)
		}
	}
}

func TestGraphSubgraphBoundsAndTopology(t *testing.T) {
	h := (&Server{Mode: "demo"}).Handler()
	for _, tc := range []struct {
		query  string
		status int
	}{
		{"node=rt-04&depth=0", 200}, {"node=rt-04&depth=1", 200},
		{"node=rt-04&depth=-1", 422}, {"node=rt-04&depth=33", 422}, {"node=rt-04&depth=1junk", 422},
		{"node=missing&depth=1", 404},
	} {
		w := durableRequest(h, "GET", "/graph/subgraph?"+tc.query, "qa", "")
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.query, w.Code, w.Body)
		}
		if tc.query == "node=rt-04&depth=1" {
			m := decodeMap(t, w)
			if len(arr(m["nodes"])) != 2 || len(arr(m["edges"])) != 1 {
				t.Fatalf("wrong directed traversal: %v", m)
			}
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/graph/subgraph?node=rt-04", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("graph auth=%d", w.Code)
	}
}

func TestCSVAuditPaginationCursor(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := db.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("pagination-%d-", time.Now().UnixNano())
	for i := 0; i < 1005; i++ {
		_, err := db.Update(ctx, "csv-test", "cursor", func(_ *store.Tx, _ json.RawMessage) (store.Change, error) {
			return store.Change{Value: i, Log: map[string]any{"kind": "rencana", "title": fmt.Sprintf("%s%04d", prefix, i)}}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	h := (&Server{Mode: "demo", DB: db}).Handler()
	w := durableRequest(h, "GET", "/audit.csv?kind=rencana", "planner", "")
	if w.Code != 200 {
		t.Fatalf("CSV: %d %s", w.Code, w.Body)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\xef\xbb\xbf"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, row := range rows[1:] {
		if strings.HasPrefix(row[2], prefix) {
			if seen[row[2]] {
				t.Fatalf("duplicate CSV row: %s", row[2])
			}
			seen[row[2]] = true
		}
	}
	if len(seen) != 1005 {
		t.Fatalf("CSV truncated: got %d/1005", len(seen))
	}
	w = durableRequest(h, "GET", "/audit.csv", "planner", "")
	if w.Code != 200 {
		t.Fatalf("audit.csv failed: %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	if !strings.HasPrefix(body, "\xef\xbb\xbf") {
		t.Fatal("missing UTF-8 BOM in audit.csv")
	}
	r := csv.NewReader(strings.NewReader(body[3:]))
	header, err := r.Read()
	if err != nil || len(header) != 7 {
		t.Fatalf("invalid CSV header: %v %v", header, err)
	}
}
