package api

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"milkgraph/internal/rules"
)

func TestFormatLimitsDiffMatchesFrontend(t *testing.T) {
	old := rules.DefaultLimits
	updated := rules.Limits{
		MaxTransfer: 10200, StarvationTol: 0, MaintenanceBay3: true,
		HoldingMin: 620, BufferMaxMin: 1440, CIPBuffer: 5, BreakdownF1: true,
		TankMax: 100, ChangeoverMax: 4,
	}
	const want = "Max transfer rate UHT ke aseptic tank 9.000 L/jam → 10.200 L/jam; Toleransi jeda pasokan filling 20 menit → 0 menit; Uji maintenance window Bay 3, 16.00 - 16.30 diaktifkan; Farm sampai tersimpan dingin 10j 30m → 10j 20m; Raw Milk Buffer sampai thermal 18j 00m → 24j 00m; Jeda setelah CIP bay 0 menit → 5 menit; Uji breakdown Filling F1, 16.30 - 17.00 diaktifkan; Batas level Raw Milk Buffer 95 % → 100 %; Changeover maksimum 2 per lini per shift → 4 per lini per shift"
	if got := formatLimitsDiff(old, updated); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if got := formatLimitsDiff(old, old); got != "" {
		t.Fatalf("unchanged limits: %q", got)
	}
	old.MaintenanceBay3, old.BreakdownF1 = true, true
	const disabled = "Uji maintenance window Bay 3, 16.00 - 16.30 dimatikan; Uji breakdown Filling F1, 16.30 - 17.00 dimatikan"
	if got := formatLimitsDiff(old, rules.DefaultLimits); got != disabled {
		t.Fatalf("got %q\nwant %q", got, disabled)
	}
}

func TestValidateLimitsBoundsStepsAndToggles(t *testing.T) {
	for _, tc := range []struct {
		key            string
		min, max, step int
	}{
		{"maxTransfer", 7000, 12000, 100},
		{"starvationTol", 0, 60, 5},
		{"holdingMin", 480, 720, 10},
		{"bufferMaxMin", 720, 1440, 30},
		{"cipBuffer", 0, 30, 5},
		{"tankMax", 85, 100, 1},
		{"changeoverMax", 0, 4, 1},
	} {
		t.Run(tc.key, func(t *testing.T) {
			withValue := func(value int) rules.Limits {
				body, _ := json.Marshal(rules.DefaultLimits)
				var fields map[string]any
				if err := json.Unmarshal(body, &fields); err != nil {
					t.Fatal(err)
				}
				fields[tc.key] = value
				body, _ = json.Marshal(fields)
				var limits rules.Limits
				if err := json.Unmarshal(body, &limits); err != nil {
					t.Fatal(err)
				}
				return limits
			}
			for value := tc.min; value <= tc.max; value += tc.step {
				if err := validateLimits(withValue(value)); err != nil {
					t.Fatalf("%d: %v", value, err)
				}
			}
			bad := []int{tc.min - 1, tc.max + 1, -1, int(^uint(0) >> 1)}
			if tc.step > 1 {
				bad = append(bad, tc.min+1, tc.max-1)
			}
			for _, value := range bad {
				err := validateLimits(withValue(value))
				var detail *apiError
				if !errors.As(err, &detail) || detail.status != 422 || detail.code != "VALIDATION" || detail.field != tc.key || detail.message == "" {
					t.Fatalf("%d: expected field-specific validation, got %#v", value, err)
				}
			}
		})
	}
	for _, maintenance := range []bool{false, true} {
		for _, breakdown := range []bool{false, true} {
			limits := rules.DefaultLimits
			limits.MaintenanceBay3, limits.BreakdownF1 = maintenance, breakdown
			if err := validateLimits(limits); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestLimitSchemaMatchesFrontend(t *testing.T) {
	const expected = `[
 {"id":"flow","label":"Flow rate","what":"Kecepatan transfer dan proses membatasi throughput. Lini filling tidak boleh kehabisan pasokan.","source":"FLOW-F2","params":[
  {"key":"maxTransfer","kind":"range","label":"Max transfer rate UHT ke aseptic tank","unit":"L/jam","min":7000,"max":12000,"step":100,"default":9000},
  {"key":"starvationTol","kind":"range","label":"Toleransi jeda pasokan filling","unit":"menit","min":0,"max":60,"step":5,"default":20}]},
 {"id":"maintenance","label":"Ketersediaan & maintenance","what":"Waktu siap aset memengaruhi jadwal. Aset dalam maintenance window tidak boleh dipakai.","source":"MAINT-01","params":[
  {"key":"maintenanceBay3","kind":"toggle","label":"Uji maintenance window Bay 3, 16.00 - 16.30","hint":"Lihat apakah rencana masih layak bila Bay 3 diservis.","default":false}]},
 {"id":"holding","label":"Holding time","what":"Bahan harus diproses dalam batas waktu sebelum tahap berikutnya, diperiksa per tahap.","source":"HOLD-RT26","params":[
  {"key":"holdingMin","kind":"range","label":"Farm sampai tersimpan dingin","unit":"menit","min":480,"max":720,"step":10,"default":630},
  {"key":"bufferMaxMin","kind":"range","label":"Raw Milk Buffer sampai thermal","unit":"menit","min":720,"max":1440,"step":30,"default":1080}]},
 {"id":"cip","label":"Downtime & CIP","what":"Cleaning, kerusakan, dan stop memotong produksi.","source":"CIP-B2","params":[
  {"key":"cipBuffer","kind":"range","label":"Jeda setelah CIP bay","unit":"menit","min":0,"max":30,"step":5,"default":0},
  {"key":"breakdownF1","kind":"toggle","label":"Uji breakdown Filling F1, 16.30 - 17.00","hint":"Simulasi kerusakan mesin di tengah jadwal.","default":false}]},
 {"id":"tank","label":"Kapasitas tangki","what":"Ruang tangki membatasi receiving dan buffer. Tangki penuh atau CIP tidak bisa menerima.","source":"RMT-LEVEL","params":[
  {"key":"tankMax","kind":"range","label":"Batas level Raw Milk Buffer","unit":"%","min":85,"max":100,"step":1,"default":95}]},
 {"id":"changeover","label":"Changeover paper/size/flavor","what":"Transisi produk mengikuti SOP: urutan dan waktu cleaning minimum untuk paper, size, dan flavor.","source":"FILL-F1","params":[
  {"key":"changeoverMax","kind":"range","label":"Changeover maksimum","unit":"per lini per shift","min":0,"max":4,"step":1,"default":2}]}
]`
	actual, err := json.Marshal((&Server{}).limitSchema())
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(actual, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema does not match frontend:\ngot %s\nwant %s", actual, expected)
	}
}
