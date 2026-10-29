package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const middlewareSecret = "0123456789abcdef0123456789abcdef"

func middlewareToken(t *testing.T, secret string, header, claims any) string {
	t.Helper()
	encode := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	body := encode(header) + "." + encode(claims)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func middlewareClaims() map[string]any {
	return map[string]any{"sub": "user-1", "role": "planner", "name": "Rina Wulandari", "exp": time.Now().Unix() + 300}
}

func TestMiddlewareHTTPHardening(t *testing.T) {
	s := &Server{Origins: []string{"https://app.example"}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/panic" {
			panic("secret-token-value")
		}
		if r.URL.Path == "/body" {
			if _, err := io.ReadAll(r.Body); err != nil {
				http.Error(w, "too large", http.StatusRequestEntityTooLarge)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	h := s.middleware(next)
	do := func(method, path, origin string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, body)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	t.Run("allowed origin", func(t *testing.T) {
		w := do("GET", "/x", "https://app.example", nil, nil)
		if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "https://app.example" {
			t.Fatalf("%d %v", w.Code, w.Header())
		}
		if !strings.Contains(w.Header().Get("Vary"), "Origin") {
			t.Fatal("missing Vary Origin")
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%v", w.Header())
		}
	})
	t.Run("no origin has no allow-origin and never wildcard", func(t *testing.T) {
		w := do("GET", "/x", "", nil, nil)
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("%v", w.Header())
		}
		if !strings.Contains(w.Header().Get("Vary"), "Origin") {
			t.Fatal("missing Vary Origin")
		}
	})
	t.Run("unmatched origin 403", func(t *testing.T) {
		w := do("GET", "/x", "https://evil.example", nil, nil)
		if w.Code != 403 || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("%d %v", w.Code, w.Header())
		}
	})
	t.Run("empty allowlist rejects any origin", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/x", nil)
		r.Header.Set("Origin", "https://app.example")
		w := httptest.NewRecorder()
		(&Server{}).middleware(next).ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
	})
	t.Run("wildcard in allowlist never emitted", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/x", nil)
		r.Header.Set("Origin", "https://evil.example")
		w := httptest.NewRecorder()
		(&Server{Origins: []string{"*"}}).middleware(next).ServeHTTP(w, r)
		if w.Code != 403 || w.Header().Get("Access-Control-Allow-Origin") == "*" {
			t.Fatalf("%d %v", w.Code, w.Header())
		}
	})
	t.Run("preflight", func(t *testing.T) {
		w := do("OPTIONS", "/x", "https://app.example", nil, map[string]string{"Access-Control-Request-Method": "PUT", "Access-Control-Request-Headers": "authorization"})
		if w.Code != 204 {
			t.Fatal(w.Code)
		}
		for _, m := range []string{"GET", "POST", "PUT", "OPTIONS"} {
			if !strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), m) {
				t.Fatalf("missing %s: %v", m, w.Header())
			}
		}
		if w.Header().Get("Access-Control-Allow-Origin") != "https://app.example" {
			t.Fatal(w.Header())
		}
	})
	t.Run("preflight unmatched origin", func(t *testing.T) {
		if w := do("OPTIONS", "/x", "https://evil.example", nil, nil); w.Code != 403 {
			t.Fatal(w.Code)
		}
	})
	t.Run("request id", func(t *testing.T) {
		a, b := do("GET", "/x", "", nil, nil).Header().Get("X-Request-ID"), do("GET", "/x", "", nil, nil).Header().Get("X-Request-ID")
		if len(a) != 32 || a == b {
			t.Fatalf("%q %q", a, b)
		}
	})
	t.Run("body limit 64KiB", func(t *testing.T) {
		if w := do("POST", "/body", "", strings.NewReader(strings.Repeat("x", 64*1024)), nil); w.Code != 204 {
			t.Fatal(w.Code)
		}
		if w := do("POST", "/body", "", strings.NewReader(strings.Repeat("x", 64*1024+1)), nil); w.Code != 413 {
			t.Fatal(w.Code)
		}
	})
	t.Run("panic recovered without leaking", func(t *testing.T) {
		w := do("GET", "/panic", "", nil, nil)
		if w.Code != 500 || strings.Contains(w.Body.String(), "secret-token-value") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
}

func TestAuthenticateModesAndHeaders(t *testing.T) {
	sLive := &Server{Mode: "live", JWTSecret: middlewareSecret}
	token := middlewareToken(t, middlewareSecret, map[string]any{"alg": "HS256"}, middlewareClaims())

	t.Run("live demo header rejected even with bearer", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/session", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Demo-Role", "planner")
		_, err := sLive.authenticate(req)
		if err == nil {
			t.Fatal("expected error")
		}
		var apiErr *apiError
		if !errors.As(err, &apiErr) || apiErr.status != 400 || apiErr.code != "BAD_REQUEST" {
			t.Fatalf("expected 400 BAD_REQUEST, got %+v", err)
		}
	})

	t.Run("live malformed bearer rejected", func(t *testing.T) {
		for _, bad := range []string{"Bearer", "Bearer ", "Bearer a b", "Basic 12345", "Token 12345"} {
			req := httptest.NewRequest("GET", "/api/v1/session", nil)
			req.Header.Set("Authorization", bad)
			_, err := sLive.authenticate(req)
			if err == nil {
				t.Fatalf("expected error for header %q", bad)
			}
		}
	})

	t.Run("demo default mode and role", func(t *testing.T) {
		sEmpty := &Server{Mode: ""}
		req := httptest.NewRequest("GET", "/api/v1/session", nil)
		req.Header.Set("X-Demo-Role", "qa")
		auth, err := sEmpty.authenticate(req)
		if err != nil {
			t.Fatal(err)
		}
		if auth.Role != "qa" || auth.Name != "Sri Lestari" {
			t.Fatalf("unexpected auth: %+v", auth)
		}
	})

	t.Run("demo invalid role rejected", func(t *testing.T) {
		sDemo := &Server{Mode: "demo"}
		req := httptest.NewRequest("GET", "/api/v1/session", nil)
		req.Header.Set("X-Demo-Role", "unknown")
		_, err := sDemo.authenticate(req)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("unknown mode fails closed", func(t *testing.T) {
		sStaging := &Server{Mode: "staging", JWTSecret: middlewareSecret}
		req := httptest.NewRequest("GET", "/api/v1/session", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		_, err := sStaging.authenticate(req)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestMiddlewareJWTClaims(t *testing.T) {
	s := &Server{JWTSecret: middlewareSecret, JWTIssuer: "milkgraph", JWTAudience: "web"}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		valid  bool
	}{
		{"valid", func(c map[string]any) {}, true},
		{"aud array", func(c map[string]any) { c["aud"] = []string{"other", "web"} }, true},
		{"missing exp", func(c map[string]any) { delete(c, "exp") }, false},
		{"expired", func(c map[string]any) { c["exp"] = time.Now().Unix() - 1 }, false},
		{"expiry boundary", func(c map[string]any) { c["exp"] = time.Now().Unix() }, false},
		{"string exp", func(c map[string]any) { c["exp"] = "9999999999" }, false},
		{"future nbf", func(c map[string]any) { c["nbf"] = time.Now().Unix() + 300 }, false},
		{"present nbf", func(c map[string]any) { c["nbf"] = time.Now().Unix() - 1 }, true},
		{"null nbf", func(c map[string]any) { c["nbf"] = nil }, false},
		{"missing sub", func(c map[string]any) { delete(c, "sub") }, false},
		{"blank sub", func(c map[string]any) { c["sub"] = "  " }, false},
		{"missing role", func(c map[string]any) { delete(c, "role") }, false},
		{"invalid role", func(c map[string]any) { c["role"] = "admin" }, false},
		{"missing name", func(c map[string]any) { delete(c, "name") }, false},
		{"blank name", func(c map[string]any) { c["name"] = "  " }, false},
		{"control name", func(c map[string]any) { c["name"] = "Rina\nforged" }, false},
		{"long name", func(c map[string]any) { c["name"] = strings.Repeat("a", 201) }, false},
		{"missing issuer", func(c map[string]any) { delete(c, "iss") }, false},
		{"wrong issuer", func(c map[string]any) { c["iss"] = "other" }, false},
		{"missing audience", func(c map[string]any) { delete(c, "aud") }, false},
		{"wrong audience", func(c map[string]any) { c["aud"] = "other" }, false},
		{"mixed audience", func(c map[string]any) { c["aud"] = []any{"web", 123} }, false},
		{"oversized", func(c map[string]any) { c["padding"] = strings.Repeat("x", 9000) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := middlewareClaims()
			c["iss"] = "milkgraph"
			c["aud"] = "web"
			tc.change(c)
			token := middlewareToken(t, middlewareSecret, map[string]any{"alg": "HS256"}, c)
			a, err := s.verifyJWT(token)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v auth=%v error=%v", tc.valid, a, err)
			}
		})
	}
}

func TestMiddlewareJWTSecretAndAlgorithm(t *testing.T) {
	for _, tc := range []struct {
		name, configured, signed, algorithm string
		valid                               bool
	}{
		{"HS256", middlewareSecret, middlewareSecret, "HS256", true},
		{"missing secret", "", "milkgraph-secret-key-production-change-me", "HS256", false},
		{"short secret", "short", "short", "HS256", false},
		{"wrong key", middlewareSecret, middlewareSecret + "bad", "HS256", false},
		{"none", middlewareSecret, middlewareSecret, "none", false},
		{"HS512", middlewareSecret, middlewareSecret, "HS512", false},
		{"missing alg", middlewareSecret, middlewareSecret, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{JWTSecret: tc.configured}
			token := middlewareToken(t, tc.signed, map[string]any{"alg": tc.algorithm, "typ": "JWT"}, middlewareClaims())
			_, err := s.verifyJWT(token)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}
