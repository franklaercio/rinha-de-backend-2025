package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"rinha-de-backend-2025/internal/cache"
	"rinha-de-backend-2025/internal/db"
	"rinha-de-backend-2025/internal/payments"
	"strconv"

	_ "github.com/lib/pq"
)

func main() {
	dbHost := getEnv("DB_HOST", "localhost")
	dbUser := getEnv("DB_USER", "postgres")
	dbPassword := getEnv("DB_PASSWORD", "postgres")
	dbName := getEnv("DB_NAME", "rinha")
	dsn := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable", dbUser, dbPassword, dbHost, dbName)

	repo, err := db.NewPostgresRepository(dsn)
	if err != nil {
		log.Fatalf("erro ao conectar ao banco: %v", err)
	}

	redisHost := getEnv("REDIS_HOST", "localhost")
	redisPort := getEnv("REDIS_PORT", "6379")
	redisClient, err := cache.NewRedisClient(redisHost, redisPort, "")
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	paymentURLDefault := getEnv("PAYMENT_URL_DEFAULT", "http://localhost:8001")
	paymentURLFallback := getEnv("PAYMENT_URL_FALLBACK", "http://localhost:8002")

	workerCountStr := getEnv("PAYMENT_WORKERS", "50")
	workerCount, err := strconv.Atoi(workerCountStr)
	if err != nil || workerCount <= 0 {
		log.Printf("Invalid PAYMENT_WORKERS value, defaulting to 50. Error: %v", err)
		workerCount = 50
	}

	service := payments.NewService(workerCount, repo, redisClient, paymentURLDefault, paymentURLFallback)
	paymentHandler := payments.NewHandler(service)

	mux := http.NewServeMux()
	mux.HandleFunc("/payments", paymentHandler.SendPayment)
	mux.HandleFunc("/payments-summary", paymentHandler.GetSummary)

	serverPort := getEnv("HTTP_PORT", "9999")
	log.Printf("Starting server at :%s", serverPort)
	if err := http.ListenAndServe(":"+serverPort, mux); err != nil {
		log.Fatalf("Could not start server: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
