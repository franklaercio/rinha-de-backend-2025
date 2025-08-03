package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"rinha-de-backend-2025/core/service"
	"rinha-de-backend-2025/core/worker"
	"rinha-de-backend-2025/infra/db"
	"rinha-de-backend-2025/infra/externalapi"
	"rinha-de-backend-2025/infra/redis"
	"rinha-de-backend-2025/internal/config"
	"rinha-de-backend-2025/internal/handler"
)

func main() {
	cfg := config.Load()

	dsn := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBName)

	postgres, err := db.NewPostgresRepository(context.Background(), dsn, cfg.DBMaxConnections)
	if err != nil {
		log.Fatalf("Erro ao conectar ao banco: %v", err)
	}

	redisClient, err := redis.NewRedisClient(cfg.RedisHost, cfg.RedisPort, "")
	if err != nil {
		log.Fatalf("Erro ao conectar ao Redis: %v", err)
	}

	paymentService := service.NewPaymentService(postgres, redisClient)
	paymentHandler := handler.NewHandler(paymentService)

	apiClient := externalapi.NewClient(postgres, cfg.PaymentURLDefault, cfg.PaymentURLFallback)
	_ = worker.NewWorker(cfg.PaymentWorkers, postgres, redisClient, paymentService, apiClient)

	mux := http.NewServeMux()

	mux.HandleFunc("/payments", paymentHandler.SendPayment)
	mux.HandleFunc("/payments-summary", paymentHandler.GetSummary)

	log.Printf("Servidor iniciado na porta :%s", cfg.HTTPPort)
	if err := http.ListenAndServe(":"+cfg.HTTPPort, mux); err != nil {
		log.Fatalf("Erro ao iniciar servidor: %v", err)
	}
}
