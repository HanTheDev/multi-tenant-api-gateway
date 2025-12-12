package config

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL string
	RedisURL    string
	JWTSecret   string
	ServerPort  string
}

func Load() (*Config, error) {
	godotenv.Load()

	return &Config{
		DatabaseURL: getEnv("DATABASE_URL", ""),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6379"),
		JWTSecret:   getEnv("JWT_SECRET", "secret"),
		ServerPort:  getEnv("SERVER_PORT", "8080"),
	}, nil
}

func getEnv(key, defaultVal string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultVal
}
