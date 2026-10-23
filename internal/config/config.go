// Package config reads LowLex settings from the environment and an optional .env file.
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultPort is the port LowLex listens on when LOWLEX_PORT is not set.
const DefaultPort = 4747

type Config struct {
	Host          string
	Port          int
	DataDir       string
	PublicURL     string
	AdminUser     string
	AdminPassword string // when set, the first admin is created with it and is not asked to change it
	Secret        string // optional; otherwise a key file is generated in DataDir
	SecureCookies bool
	LogRequests   bool
}

// Load reads a .env file (current directory, then next to the executable) without
// overriding variables that are already set, then builds the configuration.
func Load() Config {
	loadDotEnv(".env")
	if exe, err := os.Executable(); err == nil {
		loadDotEnv(filepath.Join(filepath.Dir(exe), ".env"))
	}

	c := Config{
		Host:          env("LOWLEX_HOST", "0.0.0.0"),
		Port:          DefaultPort,
		DataDir:       env("LOWLEX_DATA_DIR", "lowlex_data"),
		PublicURL:     strings.TrimRight(env("LOWLEX_PUBLIC_URL", ""), "/"),
		AdminUser:     env("LOWLEX_ADMIN_USER", "admin"),
		AdminPassword: os.Getenv("LOWLEX_ADMIN_PASSWORD"),
		Secret:        os.Getenv("LOWLEX_SECRET"),
		LogRequests:   envBool("LOWLEX_LOG_REQUESTS", false),
	}
	if p, err := strconv.Atoi(os.Getenv("LOWLEX_PORT")); err == nil && p > 0 && p < 65536 {
		c.Port = p
	}
	c.SecureCookies = envBool("LOWLEX_SECURE_COOKIES", strings.HasPrefix(c.PublicURL, "https://"))
	return c
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		} else if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
}
