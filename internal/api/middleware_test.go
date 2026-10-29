package api

import (
 "crypto/hmac"
 "crypto/sha256"
 "encoding/base64"
 "encoding/json"
 "strings"
 "testing"
 "time"
)

const middlewareSecret = "0123456789abcdef0123456789abcdef"

func middlewareToken(t *testing.T, secret string, header, claims any) string {
 t.Helper()
 encode := func(v any) string { b, err := json.Marshal(v); if err != nil { t.Fatal(err) }; return base64.RawURLEncoding.EncodeToString(b) }
 body := encode(header)+"."+encode(claims)
 mac := hmac.New(sha256.New, []byte(secret)); _, _ = mac.Write([]byte(body))
 return body+"."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func middlewareClaims() map[string]any {
 return map[string]any{"sub":"user-1", "role":"planner", "name":"Rina Wulandari", "exp":time.Now().Unix()+300}
}

func TestMiddlewareJWTClaims(t *testing.T) {
 s := &Server{JWTSecret: middlewareSecret, JWTIssuer:"milkgraph", JWTAudience:"web"}
 for _, tc := range []struct{name string; change func(map[string]any); valid bool}{
  {"valid", func(c map[string]any){}, true},
  {"aud array", func(c map[string]any){c["aud"]=[]string{"other","web"}}, true},
  {"missing exp", func(c map[string]any){delete(c,"exp")}, false},
  {"expired", func(c map[string]any){c["exp"]=time.Now().Unix()-1}, false},
  {"expiry boundary", func(c map[string]any){c["exp"]=time.Now().Unix()}, false},
  {"string exp", func(c map[string]any){c["exp"]="9999999999"}, false},
  {"future nbf", func(c map[string]any){c["nbf"]=time.Now().Unix()+300}, false},
  {"present nbf", func(c map[string]any){c["nbf"]=time.Now().Unix()-1}, true},
  {"null nbf", func(c map[string]any){c["nbf"]=nil}, false},
  {"missing sub", func(c map[string]any){delete(c,"sub")}, false},
  {"blank sub", func(c map[string]any){c["sub"]="  "}, false},
  {"missing role", func(c map[string]any){delete(c,"role")}, false},
  {"invalid role", func(c map[string]any){c["role"]="admin"}, false},
  {"missing name", func(c map[string]any){delete(c,"name")}, false},
  {"blank name", func(c map[string]any){c["name"]="  "}, false},
  {"control name", func(c map[string]any){c["name"]="Rina\nforged"}, false},
  {"long name", func(c map[string]any){c["name"]=strings.Repeat("a",201)}, false},
  {"missing issuer", func(c map[string]any){delete(c,"iss")}, false},
  {"wrong issuer", func(c map[string]any){c["iss"]="other"}, false},
  {"missing audience", func(c map[string]any){delete(c,"aud")}, false},
  {"wrong audience", func(c map[string]any){c["aud"]="other"}, false},
  {"mixed audience", func(c map[string]any){c["aud"]=[]any{"web",123}}, false},
  {"oversized", func(c map[string]any){c["padding"]=strings.Repeat("x",9000)}, false},
 } {
  t.Run(tc.name,func(t *testing.T){
   c:=middlewareClaims();c["iss"]="milkgraph";c["aud"]="web";tc.change(c)
   token:=middlewareToken(t,middlewareSecret,map[string]any{"alg":"HS256"},c)
   a,err:=s.verifyJWT(token)
   if (err==nil)!=tc.valid {t.Fatalf("valid=%v auth=%v error=%v",tc.valid,a,err)}
  })
 }
}

func TestMiddlewareJWTSecretAndAlgorithm(t *testing.T) {
 for _, tc := range []struct{name, configured, signed, algorithm string; valid bool}{
  {"HS256", middlewareSecret, middlewareSecret, "HS256", true},
  {"missing secret", "", "milkgraph-secret-key-production-change-me", "HS256", false},
  {"short secret", "short", "short", "HS256", false},
  {"wrong key", middlewareSecret, middlewareSecret+"bad", "HS256", false},
  {"none", middlewareSecret, middlewareSecret, "none", false},
  {"HS512", middlewareSecret, middlewareSecret, "HS512", false},
  {"missing alg", middlewareSecret, middlewareSecret, "", false},
 } {
  t.Run(tc.name, func(t *testing.T) {
   s := &Server{JWTSecret:tc.configured}
   token := middlewareToken(t, tc.signed, map[string]any{"alg":tc.algorithm,"typ":"JWT"}, middlewareClaims())
   _, err := s.verifyJWT(token)
   if (err == nil) != tc.valid { t.Fatalf("valid=%v error=%v", tc.valid, err) }
  })
 }
}
