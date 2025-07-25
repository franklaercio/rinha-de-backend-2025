package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"rinha-de-backend-2025/internal/broker"
	"rinha-de-backend-2025/internal/db"
	"rinha-de-backend-2025/internal/payments"
	"strconv"

	_ "github.com/lib/pq"
)

func main() {
	dbHost := getEnv("DB_HOST", "localhost")
	dsn := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable",
		getEnv("DB_USER", "postgres"),
		getEnv("DB_PASSWORD", "postgres"),
		dbHost,
		getEnv("DB_NAME", "rinha"),
	)

	repo, err := db.NewPostgresRepository(dsn)
	if err != nil {
		log.Fatalf("erro ao conectar ao banco: %v", err)
	}

	rabbitURL := getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	rabbitChannel, err := broker.NewRabbitMQChannel(rabbitURL)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}

	paymentURLDefault := getEnv("PAYMENT_URL_DEFAULT", "http://localhost:8001")
	paymentURLFallback := getEnv("PAYMENT_URL_FALLBACK", "http://localhost:8002")
	workerCount, _ := strconv.Atoi(getEnv("PAYMENT_WORKERS", "50"))
	serverPort := getEnv("HTTP_PORT", "9999")

	service := payments.NewService(workerCount, repo, rabbitChannel, paymentURLDefault, paymentURLFallback)
	paymentHandler := payments.NewHandler(service)

	mux := http.NewServeMux()
	mux.HandleFunc("/payments", paymentHandler.SendPayment)
	mux.HandleFunc("/payments-summary", paymentHandler.GetSummary)

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
