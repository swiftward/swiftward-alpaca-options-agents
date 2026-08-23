// Package config reads the settings every application here needs from the
// environment. Nothing in this repository holds a credential in a file.
package config

import "os"

type Config struct {
	Addr        string
	DatabaseURL string
	GatewayURL  string
	RecorderURL string
}

func Load() Config {
	return Config{
		Addr:        env("ADDR", ":8080"),
		DatabaseURL: env("DATABASE_URL", ""),
		GatewayURL:  env("GATEWAY_URL", ""),
		RecorderURL: env("RECORDER_URL", "http://localhost:8080"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
