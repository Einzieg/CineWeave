package config

import (
	"os"
	"strconv"
	"time"
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
