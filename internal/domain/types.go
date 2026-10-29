package domain

import (
	"encoding/json"
	"milkgraph/internal/rules"
)

type Tone string

const (
	ToneOK      Tone = "ok"
	ToneWarn    Tone = "warn"
	ToneCrit    Tone = "crit"
	ToneNeutral Tone = "neutral"
)

type RoleId string

const (
	RolePlanner    RoleId = "planner"
	RoleReceiving  RoleId = "receiving"
	RoleSupervisor RoleId = "supervisor"
	RoleQA         RoleId = "qa"
)

type Permission string

const (
	PermDemandSend     Permission = "demand.send"
	PermPlanDecide     Permission = "plan.decide"
	PermConstraintEdit Permission = "constraint.edit"
	PermInstructionAck Permission = "instruction.ack"
	PermHoldSubmit     Permission = "hold.submit"
	PermHoldReview     Permission = "hold.review"
	PermLabDecide      Permission = "lab.decide"
)

type RoleDef struct {
	Label    string `json:"label"`
	User     string `json:"user"`
	Initials string `json:"initials"`
	Focus    string `json:"focus"`
}

type PermDef struct {
	Label string   `json:"label"`
	Roles []RoleId `json:"roles"`
}

type UserInfo struct {
	Name     string `json:"name"`
	Role     string `json:"role"`
	Initials string `json:"initials"`
	Shift    string `json:"shift"`
}

type Session struct {
	User        UserInfo     `json:"user"`
	Role        RoleId       `json:"role"`
	Permissions []Permission `json:"permissions"`
}

type RolesResponse struct {
	Roles       map[RoleId]RoleDef     `json:"roles"`
	Permissions map[Permission]PermDef `json:"permissions"`
}

type Notification struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	SourceID string `json:"sourceId"`
	Tone     Tone   `json:"tone"`
}

type Shift struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Window    string `json:"window"`
	Date      string `json:"date"`
	Selected  bool   `json:"selected"`
	Available bool   `json:"available"`
}

type Source struct {
	ID     string  `json:"id"`
	Time   string  `json:"time"`
	Title  string  `json:"title"`
	Detail string  `json:"detail"`
	Node   *string `json:"node,omitempty"`
}

type Stage struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Count  int    `json:"count"`
	Status string `json:"status"`
	Tone   Tone   `json:"tone"`
}

type Tanker struct {
	ID          string  `json:"id"`
	Farm        string  `json:"farm"`
	Volume      int     `json:"volume"`
	Stage       string  `json:"stage"`
	Wait        int     `json:"wait"`
	Age         string  `json:"age"`
	Temperature float64 `json:"temperature"`
	Tone        Tone    `json:"tone"`
	Source      string  `json:"source"`
}

type Asset struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Status   string `json:"status"`
	Fill     int    `json:"fill"`
	Capacity int    `json:"capacity"`
	Unit     string `json:"unit"`
	Info     string `json:"info"`
	Tone     Tone   `json:"tone"`
	Source   string `json:"source"`
}

type IncidentFactor struct {
	Label    string `json:"label"`
	SourceID string `json:"sourceId"`
}

type IncidentInfo struct {
	PlanID         string           `json:"planId"`
	RT             string           `json:"rt"`
	HoldingLeftMin int              `json:"holdingLeftMin"`
	Summary        string           `json:"summary"`
	Factors        []IncidentFactor `json:"factors"`
}

type ReceivingView struct {
	Metrics    []map[string]any `json:"metrics"`
	Stages     []Stage          `json:"stages"`
	Tankers    []Tanker         `json:"tankers"`
	Assets     []Asset          `json:"assets"`
	LabHistory []map[string]any `json:"labHistory"`
	LabSummary map[string]any   `json:"labSummary"`
	Incident   IncidentInfo     `json:"incident"`
}

type PlanView struct {
	ID          string        `json:"id"`
	Label       string        `json:"label"`
	Detail      string        `json:"detail"`
	SolveTime   string        `json:"solveTime"`
	Turnaround  [2]string     `json:"turnaround"`
	Queue       [2]int        `json:"queue"`
	Before      []rules.Block `json:"before"`
	After       []rules.Block `json:"after"`
	Constraints []any         `json:"constraints"`
	Reasons     []any         `json:"reasons"`
	Resources   []string      `json:"resources"`
	Horizon     struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"horizon"`
	QueueLabel string `json:"queueLabel"`
}

type InstructionView struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Text    string `json:"text"`
	From    string `json:"from"`
	AckedBy string `json:"ackedBy,omitempty"`
	AckedAt string `json:"ackedAt,omitempty"`
}

type DemandSendRecord struct {
	Sent   bool   `json:"sent"`
	At     string `json:"at,omitempty"`
	By     string `json:"by,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type PlanDecision struct {
	Plan   string `json:"plan"`
	Status string `json:"status"`
	Note   string `json:"note"`
	By     string `json:"by"`
	At     string `json:"at"`
}

type Lot struct {
	ID      string `json:"id"`
	Product string `json:"product"`
	Percent int    `json:"percent"`
	Packs   int    `json:"packs"`
}

type HoldReview struct {
	Note string `json:"note"`
	By   string `json:"by"`
	At   string `json:"at"`
}

type Hold struct {
	ID      string      `json:"id"`
	Trigger string      `json:"trigger"`
	RT      string      `json:"rt"`
	Lots    []Lot       `json:"lots"`
	Note    string      `json:"note"`
	Status  string      `json:"status"`
	By      string      `json:"by"`
	At      string      `json:"at"`
	Review  *HoldReview `json:"review,omitempty"`
}

type LabDecision struct {
	Status string `json:"status"`
	Note   string `json:"note"`
	By     string `json:"by"`
	At     string `json:"at"`
}

type LabResultView struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Subject  string          `json:"subject"`
	Detail   string          `json:"detail"`
	Sampled  string          `json:"sampled"`
	Status   string          `json:"status"`
	Params   []any           `json:"params"`
	Source   string          `json:"source"`
	Decision *LabDecision    `json:"decision,omitempty"`
	Eta      string          `json:"eta,omitempty"`
}

type WriteResponse struct {
	Result any            `json:"result"`
	Log    map[string]any `json:"log"`
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

type LimitParamSchema struct {
	Key     string  `json:"key"`
	Kind    string  `json:"kind"`
	Label   string  `json:"label"`
	Unit    string  `json:"unit,omitempty"`
	Min     float64 `json:"min,omitempty"`
	Max     float64 `json:"max,omitempty"`
	Step    float64 `json:"step,omitempty"`
	Default any     `json:"default"`
	Hint    string  `json:"hint,omitempty"`
}

type LimitSchemaItem struct {
	ID     string             `json:"id"`
	Label  string             `json:"label"`
	What   string             `json:"what"`
	Source string             `json:"source"`
	Params []LimitParamSchema `json:"params"`
}

var RolesMap = map[RoleId]RoleDef{
	RolePlanner:    {Label: "Planner PPIC", User: "Rina Wulandari", Initials: "RW", Focus: "Rencana shift yang layak dan keputusan saat gangguan."},
	RoleReceiving:  {Label: "Koordinator receiving", User: "Dimas Prasetyo", Initials: "DP", Focus: "Urutan RT, bay yang siap, dan RT yang mendekati batas holding time."},
	RoleSupervisor: {Label: "Supervisor produksi", User: "Hendra Kusuma", Initials: "HK", Focus: "Lokasi bottleneck dan dampaknya ke lini filling."},
	RoleQA:         {Label: "QA/Lab", User: "Sri Lestari", Initials: "SL", Focus: "Hasil lab, lot terdampak beserta proporsi volume, dan daftar hold."},
}

var PermsMap = map[Permission]PermDef{
	PermDemandSend:     {Label: "Mengirim kebutuhan susu ke farm", Roles: []RoleId{RolePlanner}},
	PermPlanDecide:     {Label: "Menyetujui atau menolak rekomendasi rencana", Roles: []RoleId{RolePlanner}},
	PermConstraintEdit: {Label: "Mengubah batas constraint", Roles: []RoleId{RolePlanner}},
	PermInstructionAck: {Label: "Mengonfirmasi instruksi dari rencana yang disetujui", Roles: []RoleId{RoleReceiving, RoleSupervisor}},
	PermHoldSubmit:     {Label: "Mengajukan daftar hold dari hasil trace", Roles: []RoleId{RolePlanner, RoleSupervisor, RoleQA}},
	PermHoldReview:     {Label: "Menyetujui, menolak, atau melepas hold", Roles: []RoleId{RoleQA}},
	PermLabDecide:      {Label: "Merilis atau menolak hasil lab penerimaan RT", Roles: []RoleId{RoleQA}},
}

func WhoCan(perm Permission) string {
	def, ok := PermsMap[perm]
	if !ok {
		return "Hanya pengguna berwenang yang dapat melakukan ini."
	}
	var names []string
	for _, r := range def.Roles {
		names = append(names, RolesMap[r].Label)
	}
	if len(names) == 1 {
		return "Hanya " + names[0] + " yang dapat melakukan ini."
	}
	res := "Hanya "
	for i, name := range names {
		if i > 0 {
			res += " atau "
		}
		res += name
	}
	return res + " yang dapat melakukan ini."
}

func Can(role RoleId, perm Permission) bool {
	def, ok := PermsMap[perm]
	if !ok {
		return false
	}
	for _, r := range def.Roles {
		if r == role {
			return true
		}
	}
	return false
}

func PermissionsForRole(role RoleId) []Permission {
	var res []Permission
	for perm, def := range PermsMap {
		for _, r := range def.Roles {
			if r == role {
				res = append(res, perm)
				break
			}
		}
	}
	return res
}

func RawToStruct[T any](raw json.RawMessage) (T, error) {
	var target T
	err := json.Unmarshal(raw, &target)
	return target, err
}
