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
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	return Config{
		DatabaseURL:    getEnv("DATABASE_URL", "postgres://disapyr:disapyr@localhost:5432/disapyr?sslmode=disable"),
		Port:           getEnv("PORT", "8080"),
		BaseURL:        getEnv("BASE_URL", "http://localhost:8080"),
		MaxSecretBytes: getEnvInt64("MAX_SECRET_BYTES", 10*1024*1024),
		LogLevel:       getEnv("LOG_LEVEL", "info"),
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
