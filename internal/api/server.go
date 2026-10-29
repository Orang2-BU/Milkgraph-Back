package api

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "log/slog"
 "net/http"
 "sort"
 "strings"
 "time"

 "milkgraph/internal/sse"
 "milkgraph/internal/store"
 "milkgraph/internal/trace"
)

type Server struct {
 Mode string
 DB *store.DB
 Hub *sse.Hub
 JWTSecret string
 JWTIssuer string
 JWTAudience string
 Origins []string
 seeds *SeedData
 dyn *DynamicState
 trace *trace.Service
}
type apiError struct { status int; code, message, field string }
func (e *apiError) Error() string { return e.message }
func invalid(message, field string) error { return &apiError{422,"VALIDATION",message,field} }
func conflict() error { return &apiError{409,"CONFLICT","Keputusan atau pengajuan sudah ada.",""} }
func missing() error { return &apiError{404,"NOT_FOUND","Data tidak ditemukan.",""} }
func writeJSON(w http.ResponseWriter, status int, value any) {
 body,err:=json.Marshal(value); if err!=nil { status=500;body=[]byte(`{"error":{"code":"INTERNAL","message":"Kesalahan internal."}}`) }
 w.Header().Set("Content-Type","application/json; charset=utf-8");w.WriteHeader(status);_,_=w.Write(append(body,'\n'))
}
func fail(w http.ResponseWriter,err error) {
 e:=&apiError{500,"INTERNAL","Kesalahan internal.",""}
 if !errors.As(err,&e) { slog.Error("request failed","error",err);e=&apiError{500,"INTERNAL","Kesalahan internal.",""} }
 detail:=map[string]any{"code":e.code,"message":e.message};if e.field!="" {detail["field"]=e.field}
 writeJSON(w,e.status,map[string]any{"error":detail})
}
func decode(r *http.Request, value any) error {
 if r.Header.Get("Content-Type")!="" && !strings.HasPrefix(r.Header.Get("Content-Type"),"application/json") { return &apiError{415,"BAD_REQUEST","Content-Type harus application/json.",""} }
 d:=json.NewDecoder(io.LimitReader(r.Body,65537));d.DisallowUnknownFields()
 if err:=d.Decode(value);err!=nil { return &apiError{400,"BAD_REQUEST","JSON tidak valid.",""} }
 var extra any;if d.Decode(&extra)!=io.EOF {return &apiError{400,"BAD_REQUEST","Hanya satu objek JSON diizinkan.",""}}
 return nil
}
func clone[T any](v T) T { b,_:=json.Marshal(v);var out T;_ =json.Unmarshal(b,&out);return out }
func arr(v any) []any { a,_:=v.([]any);if a==nil {return []any{}};return a }
func obj(v any) map[string]any { m,_:=v.(map[string]any);if m==nil{return map[string]any{}};return m }
func str(v any) string { s,_:=v.(string);return s }
func num(v any) int { n,_:=v.(float64);return int(n) }
func item(list any,id string) map[string]any { for _,x:=range arr(list) {m:=obj(x);if str(m["id"])==id{return clone(m)}};return nil }
func (s *Server) Handler() http.Handler {
 seeds,err:=LoadAllSeeds();if err!=nil {panic(err)};s.seeds=seeds
 s.trace,err=trace.New();if err!=nil {panic(err)}
 mux:=http.NewServeMux()
 type endpoint func(*http.Request)(any,error)
 register:=func(pattern,perm string,f endpoint){
  mux.HandleFunc(pattern,func(w http.ResponseWriter,r *http.Request){
   if _,err:=s.authenticate(r);err!=nil {fail(w,err);return}
   if perm!="" {if err:=s.authorize(r,perm);err!=nil{fail(w,err);return}}
   value,err:=f(r);if err!=nil {fail(w,err);return};status:=200
   if r.Method=="POST" && r.URL.Path=="/api/v1/holds" {status=201;w.Header().Set("Location","/api/v1/holds/"+str(obj(obj(value)["result"])["id"]))}
   writeJSON(w,status,value)
  })
 }
 mux.HandleFunc("GET /api/v1/meta",s.handleMeta)
 mux.HandleFunc("GET /healthz",func(w http.ResponseWriter,r *http.Request){writeJSON(w,200,map[string]any{"status":"ok"})})
 mux.HandleFunc("GET /readyz",func(w http.ResponseWriter,r *http.Request){if s.DB==nil{fail(w,&apiError{503,"UNAVAILABLE","Database tidak tersedia.",""});return};ctx,cancel:=context.WithTimeout(r.Context(),2*time.Second);defer cancel();if err:=s.DB.Pool.Ping(ctx);err!=nil{fail(w,&apiError{503,"UNAVAILABLE","Database tidak tersedia.",""});return};writeJSON(w,200,map[string]any{"status":"ready"})})
 static:=map[string]any{
  "/roles":seeds.Roles,"/sources":seeds.Scenario["sources"],"/assets":seeds.Scenario["assets"],
  "/assets/cip-log":seeds.Ops["cipLog"],"/assets/cip-policy":map[string]any{"tankIntervalHours":24},"/assets/mid-tanks":seeds.Ops["midTanks"],
  "/production/fillers":seeds.Ops["fillers"],"/production/aseptic-tanks":seeds.Ops["asepticTanks"],"/production/unit-states":seeds.Ops["UNIT_STATE"],"/production/changeover-matrix":seeds.Ops["changeoverMatrix"],
  "/supply/farms":seeds.Ops["farms"],"/supply/trend":seeds.Ops["trend"],"/quality/active-event":seeds.Scenario["qcEvent"],
  "/graph":seeds.Scenario["graph"],"/site/layout":seeds.Site["layout"],"/holds/summary":map[string]any{"baseline":seeds.Scenario["holdBaseline"]},
 }
 for path,value:=range static {register("GET /api/v1"+path,"",func(r *http.Request)(any,error){return value,nil})}
 register("GET /api/v1/session","",func(r *http.Request)(any,error){a,err:=s.authenticate(r);if err!=nil{return nil,err};def:=obj(seeds.Roles["roles"])[a.Role];permissions:=[]string{};for p,x:=range obj(seeds.Roles["permissions"]) {for _,v:=range arr(obj(x)["roles"]) {if str(v)==a.Role {permissions=append(permissions,p)}}};sort.Strings(permissions);d:=obj(def);return map[string]any{"user":map[string]any{"name":a.Name,"role":d["label"],"initials":d["initials"],"shift":"Shift Siang"},"role":a.Role,"permissions":permissions},nil})
 register("GET /api/v1/shifts","",func(r *http.Request)(any,error){return []any{map[string]any{"id":"2026-10-14-siang","label":"Shift Siang","window":"14.00 - 22.00","date":"Rab 14 Okt","selected":true,"available":true}},nil})
 register("GET /api/v1/sources/{id}","",func(r *http.Request)(any,error){v:=item(seeds.Scenario["sources"],r.PathValue("id"));if v==nil{return nil,missing()};return v,nil})
 register("GET /api/v1/plans","",func(r *http.Request)(any,error){res:=[]any{};for _,p:=range arr(seeds.Scenario["plans"]){res=append(res,s.planView(obj(p)))};return res,nil})
 register("GET /api/v1/plans/{id}","",func(r *http.Request)(any,error){p:=s.plan(r.PathValue("id"));if p==nil{return nil,missing()};return s.planView(p),nil})
 register("GET /api/v1/limits/schema","",func(r *http.Request)(any,error){return s.limitSchema(),nil})
 register("GET /api/v1/supply/demand-plan","",func(r *http.Request)(any,error){return map[string]any{"lines":seeds.Ops["demandPlan"],"processLoss":seeds.Ops["PROCESS_LOSS"],"capacityTonPerDay":320,"capacityL":310680,"milkDensity":1.03},nil})
 register("GET /api/v1/assistant/templates","",func(r *http.Request)(any,error){return s.trace.Templates(),nil})
 register("POST /api/v1/assistant/ask","",func(r *http.Request)(any,error){var b struct{Question string `json:"question"`};if err:=decode(r,&b);err!=nil{return nil,err};if len(strings.TrimSpace(b.Question))==0||len([]rune(b.Question))>1000{return nil,invalid("Pertanyaan wajib 1-1000 karakter.","question")};return s.trace.Ask(b.Question),nil})
 register("GET /api/v1/trace/backward","",func(r *http.Request)(any,error){v,e:=s.trace.Backward(r.URL.Query().Get("lotId"));if e!=nil{return nil,missing()};return v,nil})
 register("GET /api/v1/trace/forward","",func(r *http.Request)(any,error){v,e:=s.trace.Forward(r.URL.Query().Get("rtId"),r.URL.Query().Get("date"));if e!=nil{return nil,missing()};return v,nil})
 register("GET /api/v1/trace/explain","",func(r *http.Request)(any,error){v,e:=s.trace.Explain(r.URL.Query().Get("resource"),r.URL.Query().Get("time"));if e!=nil{return nil,missing()};return v,nil})
 register("GET /api/v1/trace/handoffs","",func(r *http.Request)(any,error){v,e:=s.trace.Handoffs(r.URL.Query().Get("lotId"));if e!=nil{return nil,missing()};return v,nil})
 register("GET /api/v1/lots/locations","",func(r *http.Request)(any,error){ids:=strings.Split(r.URL.Query().Get("ids"),",");if len(ids)>100{return nil,invalid("Maksimal 100 lot.","ids")};res:=map[string]any{};for _,id:=range ids {v,ok:=obj(seeds.Ops["lotLocation"])[id];if !ok{return nil,missing()};res[id]=v};return res,nil})
 s.registerDynamic(register,mux)
 mux.HandleFunc("/",func(w http.ResponseWriter,r *http.Request){fail(w,missing())})
 return s.middleware(mux)
}
func (s *Server) handleMeta(w http.ResponseWriter,r *http.Request){ mode:=s.Mode;if mode==""{mode="demo"};writeJSON(w,200,map[string]any{"schemaVersion":"1","mode":mode,"dataMode":"simulation","date":"Rabu, 14 Oktober 2026","now":"14.42","horizon":map[string]any{"start":"13.00","end":"20.00","nowOffset":102},"snapshotLabel":"Snapshot 14.42 WIB"}) }
func (s *Server) plan(id string) map[string]any{return item(s.seeds.Scenario["plans"],id)}
func (s *Server) planView(p map[string]any) map[string]any {p=clone(p);p["resources"]=[]string{"Lab test","Bay 1","Bay 2","Bay 3","Filling F1","Filling F2","Filling F4"};p["horizon"]=map[string]any{"start":"13.00","end":"20.00"};p["queueLabel"]="RT antre di plant 16.00";return p}
func (s *Server) dbRequired() error {if s.DB==nil{return &apiError{503,"UNAVAILABLE","Database tidak tersedia.",""}};return nil}
var _ = fmt.Sprintf
