package config

import (
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL string
	Port        string
	Env         string
	AuthKey     string
	Host        string
}

func Load() *Config {
	log.Println("[CONFIG] Attempting to load .env file...")

	err := godotenv.Load()
	if err != nil {
		log.Println("[CONFIG] ℹ️ No .env file found, relying on system environment variables")
	} else {
		log.Println("[CONFIG] ✅ Successfully loaded .env file")
	}

	cfg := &Config{
		DatabaseURL: getEnv("DATABASE_URL", ""),
		Port:        getEnv("PORT", "8080"),
		Env:         getEnv("APP_ENV", "development"),
		AuthKey:     getEnv("AUTH_KEY", ""),
		Host:        getEnv("HOST", "localhost"),
	}

	log.Printf("[CONFIG] Environment: %s", cfg.Env)
	log.Printf("[CONFIG] Target Port: %s", cfg.Port)

	if cfg.DatabaseURL == "" {
		log.Fatal("[CONFIG] ❌ CRITICAL: DATABASE_URL is missing. Server cannot start.")
	} else {
		maskedDB := maskDBSource(cfg.DatabaseURL)
		log.Printf("[CONFIG] Database URL detected: %s", maskedDB)
	}

	if cfg.AuthKey == "" {
		log.Fatal("[CONFIG] ❌ CRITICAL: AUTH_KEY (JWT Secret) is missing. Security cannot be initialized.")
	} else {
		log.Println("[CONFIG] ✅ AUTH_KEY loaded successfully")
	}

	log.Println("[CONFIG] All configuration variables successfully initialized")
	return cfg
}

func getEnv(key, defaultValue string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		log.Printf("[CONFIG] ⚠️  Variable %s not found, using default: %s", key, defaultValue)
		return defaultValue
	}

	return value
}

func maskDBSource(dsn string) string {
	parts := strings.Split(dsn, "@")
	if len(parts) < 2 {
		return "invalid-dsn-format"
	}
	return "postgres://****:****@" + parts[1]
}
