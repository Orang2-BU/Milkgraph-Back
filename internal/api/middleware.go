package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"milkgraph/internal/domain"
)

type AuthContext struct {
	Role string
	Name string
}

func (s *Server) authenticate(r *http.Request) (*AuthContext, error) {
	mode := s.Mode
	if mode == "" {
		mode = "demo"
	}

	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		return s.verifyJWT(tokenStr)
	}

	if mode == "live" {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Header Authorization Bearer token wajib diisi untuk mode live."}
	}

	// Demo mode: accept X-Demo-Role
	demoRole := r.Header.Get("X-Demo-Role")
	if demoRole == "" {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Header X-Demo-Role wajib diisi."}
	}

	roleId := domain.RoleId(demoRole)
	def, ok := domain.RolesMap[roleId]
	if !ok {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Role tidak valid."}
	}

	return &AuthContext{
		Role: string(roleId),
		Name: def.User,
	}, nil
}

func (s *Server) verifyJWT(tokenStr string) (*AuthContext, error) {
	if len(tokenStr) > 4096 {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Ukuran token terlalu besar."}
	}

	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Token JWT tidak valid."}
	}

	secret := s.JWTSecret
	if len(secret) < 32 {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Konfigurasi JWT tidak valid."}
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	var header struct { Alg string `json:"alg"` }
	if err != nil || json.Unmarshal(headerJSON, &header) != nil || header.Alg != "HS256" {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Algoritma JWT tidak valid."}
	}

	signingInput := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Signature token tidak valid."}
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	expectedSig := mac.Sum(nil)

	if !hmac.Equal(sig, expectedSig) {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Signature token tidak cocok."}
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Payload token tidak valid."}
	}

	var rawClaims map[string]json.RawMessage
	if err := json.Unmarshal(payloadJSON, &rawClaims); err != nil {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Format payload token tidak valid."}
	}

	expRaw, ok := rawClaims["exp"]
	if !ok {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Claim exp wajib diisi."}
	}
	var exp int64
	if err := json.Unmarshal(expRaw, &exp); err != nil {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Format claim exp tidak valid."}
	}
	now := time.Now().Unix()
	if exp <= now {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Token telah kedaluwarsa."}
	}

	if nbfRaw, ok := rawClaims["nbf"]; ok {
		var nbf int64
		if err := json.Unmarshal(nbfRaw, &nbf); err != nil || string(nbfRaw) == "null" || nbf > now {
			return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Token belum aktif."}
		}
	}

	subRaw, ok := rawClaims["sub"]
	if !ok {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Claim sub wajib diisi."}
	}
	var sub string
	if err := json.Unmarshal(subRaw, &sub); err != nil || strings.TrimSpace(sub) == "" {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Claim sub tidak valid."}
	}

	roleRaw, ok := rawClaims["role"]
	if !ok {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Claim role wajib diisi."}
	}
	var role string
	if err := json.Unmarshal(roleRaw, &role); err != nil {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Claim role tidak valid."}
	}
	roleId := domain.RoleId(role)
	if _, ok := domain.RolesMap[roleId]; !ok {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Role token tidak valid."}
	}

	nameRaw, ok := rawClaims["name"]
	if !ok {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Claim name wajib diisi."}
	}
	var name string
	if err := json.Unmarshal(nameRaw, &name); err != nil || strings.TrimSpace(name) == "" || len([]rune(name)) > 100 || strings.ContainsAny(name, "\r\n") {
		return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Claim name tidak valid."}
	}

	if s.JWTIssuer != "" {
		issRaw, ok := rawClaims["iss"]
		if !ok {
			return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Issuer token wajib diisi."}
		}
		var iss string
		if err := json.Unmarshal(issRaw, &iss); err != nil || iss != s.JWTIssuer {
			return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Issuer token tidak cocok."}
		}
	}

	if s.JWTAudience != "" {
		audRaw, ok := rawClaims["aud"]
		if !ok {
			return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Audience token wajib diisi."}
		}
		var single string
		var list []string
		match := false
		if err := json.Unmarshal(audRaw, &single); err == nil {
			match = (single == s.JWTAudience)
		} else if err := json.Unmarshal(audRaw, &list); err == nil {
			for _, a := range list {
				if a == s.JWTAudience {
					match = true
					break
				}
			}
		} else {
			return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Format audience token tidak valid."}
		}
		if !match {
			return nil, &apiError{status: 401, code: "UNAUTHENTICATED", message: "Audience token tidak cocok."}
		}
	}

	return &AuthContext{
		Role: string(roleId),
		Name: strings.TrimSpace(name),
	}, nil
}

func (s *Server) authorize(r *http.Request, perm string) error {
	auth, err := s.authenticate(r)
	if err != nil {
		return err
	}
	permission := domain.Permission(perm)
	if !domain.Can(domain.RoleId(auth.Role), permission) {
		return &apiError{
			status:  403,
			code:    "FORBIDDEN",
			message: domain.WhoCan(permission),
		}
	}
	return nil
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CORS headers
		origin := r.Header.Get("Origin")
		allowedOrigin := "*"
		if len(s.Origins) > 0 {
			allowedOrigin = s.Origins[0]
			for _, o := range s.Origins {
				if o == origin {
					allowedOrigin = origin
					break
				}
			}
		}
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Demo-Role, Last-Event-ID")
		w.Header().Set("Access-Control-Expose-Headers", "Location, Retry-After")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		defer func() {
			if rec := recover(); rec != nil {
				fail(w, errors.New("terjadi panic internal"))
			}
		}()

		next.ServeHTTP(w, r)
	})
}
