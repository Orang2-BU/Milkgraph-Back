package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultBind = "127.0.0.1:8080"

type config struct {
	DatabaseURL, Mode, BindAddress    string
	JWTSecret, JWTIssuer, JWTAudience string
	Origins                           []string
}

// loadConfig validates env. Errors never include values of secrets or DSN.
func loadConfig(get func(string) string) (config, error) {
	c := config{
		DatabaseURL: get("DATABASE_URL"),
		Mode:        get("MODE"),
		BindAddress: get("BIND_ADDRESS"),
		JWTSecret:   get("JWT_SECRET"),
		JWTIssuer:   get("JWT_ISSUER"),
		JWTAudience: get("JWT_AUDIENCE"),
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL wajib diisi")
	}
	if c.Mode == "" {
		c.Mode = "demo"
	}
	if c.Mode != "demo" && c.Mode != "live" {
		return c, errors.New("MODE harus demo atau live")
	}
	if c.BindAddress == "" {
		c.BindAddress = defaultBind
	}
	if err := validBind(c.BindAddress); err != nil {
		return c, err
	}
	if c.Mode == "live" {
		if len(c.JWTSecret) < 32 {
			return c, errors.New("JWT_SECRET wajib minimal 32 karakter pada MODE=live")
		}
		if c.JWTIssuer == "" || c.JWTAudience == "" {
			return c, errors.New("JWT_ISSUER dan JWT_AUDIENCE wajib pada MODE=live")
		}
	}
	if raw := strings.TrimSpace(get("CORS_ORIGINS")); raw != "" {
		for _, o := range strings.Split(raw, ",") {
			o, err := normOrigin(strings.TrimSpace(o))
			if err != nil {
				return c, err
			}
			c.Origins = append(c.Origins, o)
		}
	}
	return c, nil
}

func validBind(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return errors.New("BIND_ADDRESS harus host:port")
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return errors.New("BIND_ADDRESS port tidak valid")
	}
	return nil
}

func normOrigin(s string) (string, error) {
	u, err := url.Parse(s)
	bad := fmt.Errorf("CORS_ORIGINS entri tidak valid (harus http(s)://host[:port]): %q", s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" ||
		u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", bad
	}
	return u.Scheme + "://" + u.Host, nil
}

// healthURL maps a bind address to a loopback URL for the in-container probe.
func healthURL(bind, path string) (string, error) {
	if !strings.HasPrefix(path, "/") {
		return "", errors.New("path healthcheck harus diawali /")
	}
	host, port, err := net.SplitHostPort(bind)
	if err != nil {
		return "", err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + path, nil
}

func probe(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", res.StatusCode)
	}
	return nil
}
