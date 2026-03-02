package config

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	DatabaseURL    string
	Port           string
	BaseURL        string
	MaxSecretBytes int64
	LogLevel       string

	// Rate limiting (requests per minute, 0 = disabled).
	RateLimitCreatePerMin int // POST / — secret creation
	RateLimitRevealPerMin int // POST /s/{token}/reveal
	RateLimitHealthPerMin int // GET|POST /health* — admin interface

	// TrustProxy, when true, reads the client IP from X-Forwarded-For.
	// Only enable this when the app is behind a trusted reverse proxy.
	TrustProxy bool

	// LogDBMinLevel is the minimum slog level written to the app_logs DB table.
	// Valid values: "debug", "info", "warn", "error". Default: "warn".
	LogDBMinLevel string

	// Admin credentials for /health (log browser + health status).
	// Both must be non-empty to enable the admin interface.
	AdminUser     string
	AdminPassword string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	createRate := getEnvInt("RATE_LIMIT_CREATE_PER_MIN", 10)
	if createRate < 0 {
		createRate = 0
	}
	revealRate := getEnvInt("RATE_LIMIT_REVEAL_PER_MIN", 20)
	if revealRate < 0 {
		revealRate = 0
	}
	healthRate := getEnvInt("RATE_LIMIT_HEALTH_PER_MIN", 5)
	if healthRate < 0 {
		healthRate = 0
	}
	return Config{
		DatabaseURL:    getEnv("DATABASE_URL", "postgres://disapyr:disapyr@localhost:5432/disapyr?sslmode=disable"),
		Port:           getEnv("PORT", "8080"),
		BaseURL:        getEnv("BASE_URL", "http://localhost:8080"),
		MaxSecretBytes: getEnvInt64("MAX_SECRET_BYTES", 10*1024*1024),
		LogLevel:       getEnv("LOG_LEVEL", "info"),

		RateLimitCreatePerMin: createRate,
		RateLimitRevealPerMin: revealRate,
		RateLimitHealthPerMin: healthRate,
		TrustProxy:            getEnv("TRUST_PROXY", "") == "true",
		LogDBMinLevel:         getEnv("LOG_DB_MIN_LEVEL", "warn"),
		AdminUser:             getEnv("ADMIN_USER", ""),
		AdminPassword:         getEnv("ADMIN_PASSWORD", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// Print writes the current configuration to w in a human-readable format.
// Sensitive fields (database password, admin password) are redacted.
func (c Config) Print(w io.Writer) {
	fmt.Fprintln(w, "=== disapyr-link configuration ===")
	fmt.Fprintf(w, "DATABASE_URL           = %s\n", redactURL(c.DatabaseURL))
	fmt.Fprintf(w, "PORT                   = %s\n", c.Port)
	fmt.Fprintf(w, "BASE_URL               = %s\n", c.BaseURL)
	fmt.Fprintf(w, "MAX_SECRET_BYTES       = %d\n", c.MaxSecretBytes)
	fmt.Fprintf(w, "LOG_LEVEL              = %s\n", c.LogLevel)
	fmt.Fprintf(w, "LOG_DB_MIN_LEVEL       = %s\n", c.LogDBMinLevel)
	fmt.Fprintf(w, "RATE_LIMIT_CREATE_PER_MIN = %d\n", c.RateLimitCreatePerMin)
	fmt.Fprintf(w, "RATE_LIMIT_REVEAL_PER_MIN = %d\n", c.RateLimitRevealPerMin)
	fmt.Fprintf(w, "RATE_LIMIT_HEALTH_PER_MIN = %d\n", c.RateLimitHealthPerMin)
	fmt.Fprintf(w, "TRUST_PROXY            = %v\n", c.TrustProxy)
	fmt.Fprintf(w, "ADMIN_USER             = %s\n", c.AdminUser)
	if c.AdminPassword != "" {
		fmt.Fprintf(w, "ADMIN_PASSWORD         = ****\n")
	} else {
		fmt.Fprintf(w, "ADMIN_PASSWORD         = (not set)\n")
	}
}

// redactURL redacts the password from DATABASE_URL regardless of format.
// Supports both URL form (postgres://user:pass@host/db) and
// key=value DSN form (host=... password=... user=...).
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.Scheme != "" {
		// URL form — use stdlib redaction (replaces password with xxxxx).
		return u.Redacted()
	}
	// Key=value DSN form — redact password= and passfile= tokens.
	return kvDSNRedact(raw)
}

// kvDSNRedact replaces the value of password= and passfile= in a key=value DSN.
func kvDSNRedact(dsn string) string {
	// Tokens are space-separated; values may be single-quoted.
	// Pattern: password=value or password='value with spaces'
	result := make([]byte, 0, len(dsn))
	i := 0
	for i < len(dsn) {
		// Find next key=value token.
		for i < len(dsn) && dsn[i] == ' ' {
			result = append(result, dsn[i])
			i++
		}
		// Scan key.
		keyStart := i
		for i < len(dsn) && dsn[i] != '=' && dsn[i] != ' ' {
			i++
		}
		key := dsn[keyStart:i]
		if i >= len(dsn) || dsn[i] != '=' {
			result = append(result, key...)
			continue
		}
		result = append(result, key...)
		result = append(result, '=')
		i++ // consume '='
		// Scan value (possibly single-quoted).
		sensitive := key == "password" || key == "passfile"
		if i < len(dsn) && dsn[i] == '\'' {
			end := i + 1
			for end < len(dsn) && dsn[end] != '\'' {
				if dsn[end] == '\\' {
					end++
				}
				end++
			}
			if end < len(dsn) {
				end++ // closing quote
			}
			if sensitive {
				result = append(result, []byte("'xxxxx'")...)
			} else {
				result = append(result, dsn[i:end]...)
			}
			i = end
		} else {
			end := i
			for end < len(dsn) && dsn[end] != ' ' {
				end++
			}
			if sensitive {
				result = append(result, []byte("xxxxx")...)
			} else {
				result = append(result, dsn[i:end]...)
			}
			i = end
		}
	}
	return string(result)
}
