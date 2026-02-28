package config

import (
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
	return Config{
		DatabaseURL:    getEnv("DATABASE_URL", "postgres://disapyr:disapyr@localhost:5432/disapyr?sslmode=disable"),
		Port:           getEnv("PORT", "8080"),
		BaseURL:        getEnv("BASE_URL", "http://localhost:8080"),
		MaxSecretBytes: getEnvInt64("MAX_SECRET_BYTES", 10*1024*1024),
		LogLevel:       getEnv("LOG_LEVEL", "info"),

		RateLimitCreatePerMin: createRate,
		RateLimitRevealPerMin: revealRate,
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
