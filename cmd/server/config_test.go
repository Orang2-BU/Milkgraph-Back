package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const secret32 = "0123456789abcdef0123456789abcdef"

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func liveEnv() map[string]string {
	return map[string]string{"DATABASE_URL": "postgres://u:p@h/db", "MODE": "live", "JWT_SECRET": secret32, "JWT_ISSUER": "iss", "JWT_AUDIENCE": "aud"}
}

func TestDefaultsDemo(t *testing.T) {
	c, err := loadConfig(env(map[string]string{"DATABASE_URL": "postgres://u:p@h/db"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode != "demo" || c.BindAddress != "127.0.0.1:8080" || len(c.Origins) != 0 {
		t.Fatalf("got %+v", c)
	}
}

func TestInvalid(t *testing.T) {
	long := func(k, v string) map[string]string { m := liveEnv(); m[k] = v; return m }
	cases := map[string]map[string]string{
		"no db":           {},
		"bad mode":        {"DATABASE_URL": "x", "MODE": "prod"},
		"bad bind":        {"DATABASE_URL": "x", "BIND_ADDRESS": "8080"},
		"bad bind port":   {"DATABASE_URL": "x", "BIND_ADDRESS": ":99999"},
		"short secret":    long("JWT_SECRET", "short"),
		"no secret":       long("JWT_SECRET", ""),
		"no issuer":       long("JWT_ISSUER", ""),
		"no audience":     long("JWT_AUDIENCE", ""),
		"wildcard cors":   long("CORS_ORIGINS", "*"),
		"ftp cors":        long("CORS_ORIGINS", "ftp://a.com"),
		"path cors":       long("CORS_ORIGINS", "https://a.com/x"),
		"no host cors":    long("CORS_ORIGINS", "https://"),
		"userinfo cors":   long("CORS_ORIGINS", "https://u@a.com"),
		"empty item cors": long("CORS_ORIGINS", "https://a.com,,"),
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadConfig(env(m))
			if err == nil {
				t.Fatal("want error")
			}
			if strings.Contains(err.Error(), secret32) || strings.Contains(err.Error(), "u:p@") {
				t.Fatalf("leak: %v", err)
			}
		})
	}
}

func TestLiveValidAndOrigins(t *testing.T) {
	m := liveEnv()
	m["CORS_ORIGINS"] = " https://a.com , http://localhost:5173/ "
	m["BIND_ADDRESS"] = ":8080"
	c, err := loadConfig(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://a.com", "http://localhost:5173"}; !reflect.DeepEqual(c.Origins, want) {
		t.Fatalf("got %v", c.Origins)
	}
}

func TestHealthURL(t *testing.T) {
	for bind, want := range map[string]string{
		"127.0.0.1:8080": "http://127.0.0.1:8080/readyz",
		":8080":          "http://127.0.0.1:8080/readyz",
		"0.0.0.0:9":      "http://127.0.0.1:9/readyz",
		"[::]:9":         "http://127.0.0.1:9/readyz",
		"[::1]:9":        "http://[::1]:9/readyz",
	} {
		got, err := healthURL(bind, "/readyz")
		if err != nil || got != want {
			t.Fatalf("%s: %q %v", bind, got, err)
		}
	}
	if _, err := healthURL(":8080", "readyz"); err == nil {
		t.Fatal("want error for path without slash")
	}
}

func TestProbe(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer bad.Close()
	if err := probe(context.Background(), ok.URL); err != nil {
		t.Fatal(err)
	}
	if err := probe(context.Background(), bad.URL); err == nil {
		t.Fatal("want error on 503")
	}
}
