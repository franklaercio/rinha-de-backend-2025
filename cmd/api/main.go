package main

import (
	"log"
	"net/http"
	"rinha-de-backend-2025/internal/db"
	"rinha-de-backend-2025/internal/payments"

	_ "github.com/lib/pq"
)

func main() {
	dsn := "postgres://postgres:postgres@localhost:5432/rinha?sslmode=disable"

	repo, err := db.NewPostgresRepository(dsn)
	if err != nil {
		log.Fatalf("erro ao conectar ao banco: %v", err)
	}

	mux := http.NewServeMux()

	service := payments.NewService(*repo, "http://localhost:8001", "http://localhost:8002")
	paymentHandler := payments.NewHandler(service)

	mux.HandleFunc("/payments", paymentHandler.SendPayment)
	mux.HandleFunc("/payments-summary", paymentHandler.GetSummary)

	log.Println("Starting server at :9999")
	if err := http.ListenAndServe(":9999", mux); err != nil {
		log.Fatalf("Could not start server: %v", err)
	}
}
