package main

import (
	"log"
	"net/http"
	"rinha-de-backend-2025/internal/handler"
)

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/hello", handler.HelloHandler)

	log.Println("Starting server at :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("Could not start server: %v", err)
	}
}
