// Package config reads service settings from environment variables.
package config

import "os"

// Env returns the value of the environment variable key, or fallback when it is unset or empty.
func Env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
