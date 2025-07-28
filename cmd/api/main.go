package main

import (
	"context"
	"fmt"
	"github.com/labstack/echo/v4"
	"log"
	"rinha-de-backend-2025/internal/cache"
	"rinha-de-backend-2025/internal/config"
	"rinha-de-backend-2025/internal/db"
	"rinha-de-backend-2025/internal/payments"
)

func main() {
	cfg := config.Load()

	dsn := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBName)

	repo, err := db.NewPostgresRepository(context.Background(), dsn, cfg.DBMaxConnections)
	if err != nil {
		log.Fatalf("Erro ao conectar ao banco: %v", err)
	}

	redisClient, err := cache.NewRedisClient(cfg.RedisHost, cfg.RedisPort, "")
	if err != nil {
		log.Fatalf("Erro ao conectar ao Redis: %v", err)
	}

	service := payments.NewService(cfg.PaymentWorkers, repo, redisClient, cfg.PaymentURLDefault, cfg.PaymentURLFallback)
	handler := payments.NewHandler(service)

	e := echo.New()
	e.POST("/payments", handler.SendPayment)
	e.GET("/payments-summary", handler.GetSummary)

	log.Printf("Servidor iniciado na porta :%s", cfg.HTTPPort)
	if err := e.Start(":" + cfg.HTTPPort); err != nil {
		log.Fatalf("Erro ao iniciar servidor: %v", err)
	}
}
