package rules

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type Limits struct {
	MaxTransfer     int  `json:"maxTransfer"`
	StarvationTol   int  `json:"starvationTol"`
	MaintenanceBay3 bool `json:"maintenanceBay3"`
	HoldingMin      int  `json:"holdingMin"`
	BufferMaxMin    int  `json:"bufferMaxMin"`
	CIPBuffer       int  `json:"cipBuffer"`
	BreakdownF1     bool `json:"breakdownF1"`
	TankMax         int  `json:"tankMax"`
	ChangeoverMax   int  `json:"changeoverMax"`
}

type Plan struct {
	ID     string  `json:"id"`
	Before []Block `json:"before"`
	After  []Block `json:"after"`
}

type Check struct {
	OK   bool   `json:"ok"`
	Text string `json:"text"`
}

type ViewCounts struct {
	Before int `json:"before"`
	After  int `json:"after"`
}

type CheckResult struct {
	Before      map[string]Check `json:"before"`
	After       map[string]Check `json:"after"`
	Violations  ViewCounts       `json:"violations"`
	Starvation  ViewCounts       `json:"starvation"`
	HoldingLeft int              `json:"holdingLeft"`
}

type Instruction struct {
	ID   string `json:"id"`
	Role string `json:"role"`
	Text string `json:"text"`
	From string `json:"from"`
}

var DefaultLimits = Limits{MaxTransfer: 9000, StarvationTol: 20, HoldingMin: 630, BufferMaxMin: 1080, TankMax: 95, ChangeoverMax: 2}

var sopChangeover = map[string]int{
	"UHT Full Cream 1 L>UHT Cokelat 200 ml":  35,
	"UHT Full Cream 1 L>UHT Stroberi 200 ml": 40,
	"UHT Cokelat 200 ml>UHT Full Cream 1 L":  45,
	"UHT Cokelat 200 ml>UHT Stroberi 200 ml": 25,
	"UHT Stroberi 200 ml>UHT Full Cream 1 L": 50,
	"UHT Stroberi 200 ml>UHT Cokelat 200 ml": 30,
}

func clock(offset int) string {
	hour := offset / 60
	if offset%60 < 0 {
		hour--
	}
	return fmt.Sprintf("%d.%02d", 13+hour, offset%60)
}
func hours(minutes int) string {
	hour := minutes / 60
	if minutes%60 < 0 {
		hour--
	}
	return fmt.Sprintf("%dj %02dm", hour, minutes%60)
}
func litres(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s + " L/jam"
}
func rt(b Block) string                     { return strings.TrimSuffix(b.Label, " bentrok CIP") }
func line(resource string) string           { return strings.Replace(resource, "Filling ", "", 1) }
func overlaps(b Block, start, end int) bool { return b.Start < end && b.End > start }
func HoldingLeft(limits Limits) int         { return limits.HoldingMin - 560 }

func CheckPlan(plan Plan, limits Limits) CheckResult {
	result := CheckResult{
		Before:      Evaluate(plan, "before", limits),
		After:       Evaluate(plan, "after", limits),
		Starvation:  ViewCounts{Before: Starvation(plan.Before), After: Starvation(plan.After)},
		HoldingLeft: HoldingLeft(limits),
	}
	for _, check := range result.Before {
		if !check.OK {
			result.Violations.Before++
		}
	}
	for _, check := range result.After {
		if !check.OK {
			result.Violations.After++
		}
	}
	return result
}

func EvaluateResult(plan Plan, limits Limits) CheckResult { return CheckPlan(plan, limits) }

func Instructions(plan Plan) []Instruction {
	unloadOf := func(blocks []Block, id string) *Block {
		for _, b := range blocks {
			if (b.Type == "unload" || b.Type == "violation") && rt(b) == id {
				c := b
				return &c
			}
		}
		return nil
	}
	moved := []Instruction{}
	for _, b := range plan.After {
		if b.Type != "unload" || !strings.HasPrefix(b.Label, "RT") {
			continue
		}
		was := unloadOf(plan.Before, b.Label)
		if was != nil && was.Resource == b.Resource && was.Start == b.Start {
			continue
		}
		from := "Belum terjadwal di rencana awal"
		if was != nil {
			from = fmt.Sprintf("Semula %s %s", was.Resource, clock(was.Start))
		}
		moved = append(moved, Instruction{
			ID:   fmt.Sprintf("%s:%s", plan.ID, b.Label),
			Role: "receiving",
			Text: fmt.Sprintf("%s bongkar di %s, %s - %s", b.Label, b.Resource, clock(b.Start), clock(b.End)),
			From: from,
		})
	}
	sort.Slice(moved, func(i, j int) bool { return moved[i].Text < moved[j].Text })
	before, after := Starvation(plan.Before), Starvation(plan.After)
	if before > after {
		moved = append(moved, Instruction{
			ID:   fmt.Sprintf("%s:F2", plan.ID),
			Role: "supervisor",
			Text: fmt.Sprintf("Filling F2 dan F4 tetap jalan, jeda pasokan AT-2 turun dari %d ke %d menit", before, after),
			From: "Rencana awal: F2 dan F4 berhenti menunggu AT-2",
		})
	}
	return moved
}

func Evaluate(plan Plan, view string, limits Limits) map[string]Check {
	blocks := plan.Before
	// ponytail: fixed scenario facts preserve rules.ts compatibility; replace with simulator facts when available.
	finish, transfer, bufferAge, tankPeak := 195, 10200, 970, 91
	if view == "after" {
		blocks = plan.After
		finish, transfer, bufferAge, tankPeak = 170, 8540, 905, 94
	}
	var unloads, gaps, cips, changes []Block
	for _, b := range blocks {
		switch b.Type {
		case "unload", "violation":
			unloads = append(unloads, b)
		case "starvation":
			gaps = append(gaps, b)
		case "cip":
			cips = append(cips, b)
		case "changeover":
			changes = append(changes, b)
		}
	}
	starved := Starvation(blocks)
	stoppedLines := []string{}
	seen := map[string]bool{}
	for _, b := range gaps {
		l := line(b.Resource)
		if !seen[l] {
			stoppedLines = append(stoppedLines, l)
			seen[l] = true
		}
	}
	stopped := strings.Join(stoppedLines, " dan ")
	rateOK := transfer <= limits.MaxTransfer
	flow := Check{OK: rateOK && starved <= limits.StarvationTol}
	if flow.OK {
		flow.Text = fmt.Sprintf("UHT-2 ke AT-2 butuh %s, dalam batas transfer %s. ", litres(transfer), litres(limits.MaxTransfer))
		if starved != 0 {
			flow.Text += fmt.Sprintf("Jeda %s %d menit, dalam toleransi %d menit [asumsi].", stopped, starved, limits.StarvationTol)
		} else {
			flow.Text += "Pasokan F2 dan F4 terjaga."
		}
	} else {
		if rateOK {
			flow.Text = fmt.Sprintf("UHT-2 ke AT-2 butuh %s, dalam batas transfer.", litres(transfer))
		} else {
			flow.Text = fmt.Sprintf("Untuk mengisi AT-2 tepat waktu, UHT-2 harus mengejar %s, melebihi batas transfer %s.", litres(transfer), litres(limits.MaxTransfer))
		}
		if starved != 0 {
			flow.Text += fmt.Sprintf(" Pasokan ke %s terputus %d menit mulai %s", stopped, starved, clock(gaps[0].Start))
			if starved > limits.StarvationTol {
				flow.Text += fmt.Sprintf(", melebihi toleransi %d menit", limits.StarvationTol)
			}
			flow.Text += "."
		}
	}

	maintenance := Check{OK: true, Text: "Tidak ada maintenance window di horizon ini."}
	if limits.MaintenanceBay3 {
		blocked := []string{}
		for _, b := range unloads {
			if b.Resource == "Bay 3" && overlaps(b, 180, 210) {
				blocked = append(blocked, rt(b))
			}
		}
		maintenance.Text = "Tidak ada bongkar saat maintenance Bay 3 16.00-16.30."
		if len(blocked) != 0 {
			maintenance.OK = false
			maintenance.Text = strings.Join(blocked, ", ") + " bongkar saat maintenance Bay 3 16.00-16.30."
		}
	}

	left := limits.HoldingMin - (560 + finish - 102)
	holding := Check{OK: left >= 0 && bufferAge <= limits.BufferMaxMin}
	if left >= 0 {
		holding.Text = fmt.Sprintf("RT-26 selesai bongkar %s, sisa %d menit sebelum batas %s [asumsi].", clock(finish), left, hours(limits.HoldingMin))
	} else {
		holding.Text = fmt.Sprintf("RT-26 baru selesai bongkar %s, lewat batas %s [asumsi] pukul %s.", clock(finish), hours(limits.HoldingMin), clock(102+HoldingLeft(limits)))
	}
	holding.Text += fmt.Sprintf(" Susu tertua di RMT-2 masuk thermal pada umur %s, batas %s", hours(bufferAge), hours(limits.BufferMaxMin))
	if bufferAge > limits.BufferMaxMin {
		holding.Text += ", terlampaui"
	}
	holding.Text += "."

	clash := []Block{}
	for _, b := range unloads {
		if !strings.HasPrefix(b.Label, "RT") {
			continue
		}
		for _, c := range cips {
			if c.Resource == b.Resource && overlaps(b, c.Start, c.End+limits.CIPBuffer) {
				clash = append(clash, b)
				break
			}
		}
	}
	cipEnd := 0
	if len(cips) != 0 {
		cipEnd = cips[0].End
	}
	for _, c := range cips {
		if c.End > cipEnd {
			cipEnd = c.End
		}
	}
	cipEnd += limits.CIPBuffer
	buffered := ""
	if limits.CIPBuffer != 0 {
		buffered = fmt.Sprintf(" + jeda %d menit", limits.CIPBuffer)
	}
	cip := Check{OK: len(clash) == 0}
	if len(clash) != 0 {
		texts := []string{}
		for _, b := range clash {
			texts = append(texts, fmt.Sprintf("%s dijadwalkan di %s %s, padahal %s CIP sampai %s%s.", rt(b), b.Resource, clock(b.Start), b.Resource, clock(cipEnd-limits.CIPBuffer), buffered))
		}
		cip.Text = strings.Join(texts, " ")
	} else {
		included := ""
		if buffered != "" {
			included = " (termasuk jeda)"
		}
		end := clock(cipEnd)
		if len(cips) == 0 {
			end = "-Infinity.NaN"
		}
		cip.Text = fmt.Sprintf("Tidak ada bongkar di Bay 2 sebelum CIP selesai %s%s.", end, included)
	}
	if limits.BreakdownF1 {
		broken := []string{}
		for _, b := range blocks {
			if b.Resource == "Filling F1" && b.Type != "starvation" && overlaps(b, 210, 240) {
				broken = append(broken, b.Label)
			}
		}
		if len(broken) != 0 {
			cip.OK = false
			cip.Text += " Breakdown F1 16.30-17.00 memotong " + strings.Join(broken, " dan ") + "; jadwal F1 perlu disusun ulang."
		} else {
			cip.Text += " Breakdown F1 16.30-17.00 tidak memotong jadwal."
		}
	}
	tank := Check{OK: tankPeak <= limits.TankMax, Text: fmt.Sprintf("Raw Milk Buffer maksimum %d%% dari kapasitas, batas %d%%. Transfer tank ST-4 siap menerima.", tankPeak, limits.TankMax)}

	perLine := 0
	for _, l := range lines {
		count := 0
		for _, b := range changes {
			if b.Resource == l {
				count++
			}
		}
		if count > perLine {
			perLine = count
		}
	}
	changeover := Check{OK: perLine <= limits.ChangeoverMax, Text: fmt.Sprintf("Tidak ada changeover. Batas %d per lini per shift.", limits.ChangeoverMax)}
	for i, b := range changes {
		var from, to *Block
		for _, x := range blocks {
			if x.Resource != b.Resource || x.Type != "process" {
				continue
			}
			if x.End <= b.Start && (from == nil || x.End > from.End) {
				from = &x
			}
			if x.Start >= b.End && (to == nil || x.Start < to.Start) {
				to = &x
			}
		}
		need := 0
		if from != nil && to != nil {
			need = sopChangeover[from.Label+">"+to.Label]
		}
		ok := b.End-b.Start >= need
		changeover.OK = changeover.OK && ok
		if i == 0 {
			status := "memenuhi"
			if !ok {
				status = "kurang dari"
			}
			changeover.Text = fmt.Sprintf("%d changeover di %s pukul %s, %d menit; %s SOP %d menit. Batas %d changeover per lini per shift.", len(changes), line(b.Resource), clock(b.Start), b.End-b.Start, status, need, limits.ChangeoverMax)
		}
	}
	return map[string]Check{"flow": flow, "maintenance": maintenance, "holding": holding, "cip": cip, "tank": tank, "changeover": changeover}
}

type Block struct {
	Resource string `json:"resource"`
	Label    string `json:"label"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Type     string `json:"type"`
	Source   string `json:"source"`
}

var lines = []string{"Filling F1", "Filling F2", "Filling F4"}

func Starvation(blocks []Block) int {
	max := 0
	for _, l := range lines {
		sum := 0
		for _, b := range blocks {
			if b.Resource == l && b.Type == "starvation" {
				sum += b.End - b.Start
			}
		}
		if sum > max {
			max = sum
		}
	}
	return max
}
