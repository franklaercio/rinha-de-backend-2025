// internal/db/redis_repository.go
package db

import (
	"context"
	"encoding/json"
	"fmt"
	"rinha-de-backend-2025/domain"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisRepository implementa a persistência usando Redis.
type RedisRepository struct {
	client *redis.Client
}

// NewRedisRepository cria uma instância do repositório Redis.
func NewRedisRepository(client *redis.Client) *RedisRepository {
	return &RedisRepository{client: client}
}

// SavePayment salva o pagamento e atualiza os contadores do sumário atomicamente.
func (r *RedisRepository) SavePayment(p domain.Payment, origin domain.PaymentProcessor) (string, error) {
	ctx := context.Background()

	// Usamos um Pipeline para garantir que as operações sejam enviadas ao Redis de uma só vez.
	pipe := r.client.Pipeline()

	// 1. Salvar os detalhes do pagamento como um JSON em um Hash (para auditoria)
	paymentKey := fmt.Sprintf("payment:%s", p.CorrelationID)
	paymentData, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("could not marshal payment: %w", err)
	}
	pipe.HSet(ctx, paymentKey, "data", paymentData)
	pipe.HSet(ctx, paymentKey, "origin", int(origin))

	// 2. Atualizar os contadores do sumário de forma atômica
	summaryKey := fmt.Sprintf("summary:%d", origin)
	pipe.HIncrBy(ctx, summaryKey, "total_requests", 1)
	// Para o valor, usamos INCRBYFLOAT para manter a precisão.
	pipe.HIncrByFloat(ctx, summaryKey, "total_amount", p.Amount)

	// Executa o pipeline
	_, err = pipe.Exec(ctx)
	if err != nil {
		return "", fmt.Errorf("could not execute redis pipeline for save: %w", err)
	}

	return p.CorrelationID, nil
}

// GetPaymentSummary lê os contadores pré-agregados do Redis. É extremamente rápido.
func (r *RedisRepository) GetPaymentSummary(ctx context.Context, from, to time.Time) (*domain.PaymentSummaryResponse, error) {
	summary := &domain.PaymentSummaryResponse{}
	origins := []domain.PaymentProcessor{domain.PaymentDefault, domain.PaymentFallback}

	for _, origin := range origins {
		summaryKey := fmt.Sprintf("summary:%d", origin)

		// Pega todos os campos do hash de uma vez
		data, err := r.client.HGetAll(ctx, summaryKey).Result()
		if err != nil {
			// Se a chave não existir, não é um erro, apenas significa 0.
			if err == redis.Nil {
				continue
			}
			return nil, fmt.Errorf("could not get summary for origin %d: %w", origin, err)
		}

		// Converte os valores de string para os tipos corretos
		totalRequests, _ := strconv.Atoi(data["total_requests"])
		totalAmount, _ := strconv.ParseFloat(data["total_amount"], 64)

		paymentSummary := domain.PaymentSummary{
			TotalRequests: totalRequests,
			TotalAmount:   totalAmount,
		}

		if origin == domain.PaymentDefault {
			summary.Default = paymentSummary
		} else {
			summary.Fallback = paymentSummary
		}
	}

	return summary, nil
}
