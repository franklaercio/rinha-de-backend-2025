// cmd/api/main.go
package main

import (
	"log"
	"net/http"
	"os"
	"rinha-de-backend-2025/internal/cache"
	"rinha-de-backend-2025/internal/db" // Continua usando o pacote db
	"rinha-de-backend-2025/internal/payments"
	"strconv"
	// REMOVA: _ "github.com/lib/pq"
)

func main() {
	// --- REMOVIDA: Lógica de conexão com o Postgres ---

	// A conexão com o Redis agora é usada para o repositório e a fila
	redisHost := getEnv("REDIS_HOST", "localhost")
	redisPort := getEnv("REDIS_PORT", "6379")
	redisClient, err := cache.NewRedisClient(redisHost, redisPort, "")
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	// --- ADICIONADO: Criação do repositório Redis ---
	repo := db.NewRedisRepository(redisClient)

	paymentURLDefault := getEnv("PAYMENT_URL_DEFAULT", "http://localhost:8001")
	paymentURLFallback := getEnv("PAYMENT_URL_FALLBACK", "http://localhost:8002")

	workerCountStr := getEnv("PAYMENT_WORKERS", "50")
	workerCount, err := strconv.Atoi(workerCountStr)
	if err != nil || workerCount <= 0 {
		log.Printf("Invalid PAYMENT_WORKERS value, defaulting to 50. Error: %v", err)
		workerCount = 50
	}

	// O service agora recebe o RedisRepository, que satisfaz a mesma interface
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
