package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultJWTSecret    = "dev-insecure-cineweave-secret"
	DefaultServiceToken = "dev-service-token"
)

type Server struct {
	Name string
	Addr string
	Env  string
}

func ServerFromEnv(name, addrKey, defaultAddr string) Server {
	return Server{
		Name: name,
		Addr: Get(addrKey, defaultAddr),
		Env:  Get("CINEWEAVE_ENV", "development"),
	}
}

func Get(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func IsProduction(env string) bool {
	return strings.EqualFold(strings.TrimSpace(env), "production")
}

func ValidateProductionSecret(env, key, value string, forbiddenValues ...string) error {
	if !IsProduction(env) {
		return nil
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s must be set when CINEWEAVE_ENV=production", key)
	}
	for _, forbidden := range forbiddenValues {
		if value == strings.TrimSpace(forbidden) {
			return fmt.Errorf("%s must not use a development default when CINEWEAVE_ENV=production", key)
		}
	}
	return nil
}

func Int(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func Duration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
