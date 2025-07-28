package config

import (
	"os"
	"strconv"
)

type Config struct {
	DBHost             string
	DBUser             string
	DBPassword         string
	DBName             string
	DBMaxConnections   int
	RedisHost          string
	RedisPort          string
	PaymentURLDefault  string
	PaymentURLFallback string
	PaymentWorkers     int
	HTTPPort           string
}

func Load() *Config {
	return &Config{
		DBHost:             getEnv("DB_HOST", "localhost"),
		DBUser:             getEnv("DB_USER", "postgres"),
		DBPassword:         getEnv("DB_PASSWORD", "postgres"),
		DBName:             getEnv("DB_NAME", "rinha"),
		DBMaxConnections:   getEnvInt("DB_MAXCONNECTIONS", 25),
		RedisHost:          getEnv("REDIS_HOST", "localhost"),
		RedisPort:          getEnv("REDIS_PORT", "6379"),
		PaymentURLDefault:  getEnv("PAYMENT_URL_DEFAULT", "http://localhost:8001"),
		PaymentURLFallback: getEnv("PAYMENT_URL_FALLBACK", "http://localhost:8002"),
		PaymentWorkers:     getEnvInt("PAYMENT_WORKERS", 50),
		HTTPPort:           getEnv("HTTP_PORT", "9999"),
	}
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if val, ok := os.LookupEnv(key); ok {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return fallback
}
