package main

import (
	"log"
	"net/http"
	"rinha-de-backend-2025/internal/handler"
	"rinha-de-backend-2025/internal/payments"
)

func main() {
	mux := http.NewServeMux()

	service := payments.NewService("http://localhost:8001")
	paymentHandler := payments.NewHandler(service)

	mux.HandleFunc("/hello", handler.HelloHandler)

	mux.Handle("/payments", paymentHandler)

	log.Println("Starting server at :9999")
	if err := http.ListenAndServe(":9999", mux); err != nil {
		log.Fatalf("Could not start server: %v", err)
	}
}
