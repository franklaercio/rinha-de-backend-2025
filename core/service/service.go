package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"rinha-de-backend-2025/core/model"
	"rinha-de-backend-2025/infra/db"
	"rinha-de-backend-2025/infra/redis"
	"time"
)

const (
	paymentQueueName = "payment_queue"
)

type PaymentService interface {
	CreatePayment(input model.Payment) error
	RetrievePaymentSummary(from, to string) (*model.PaymentSummaryResponse, error)
}

type paymentService struct {
	db          *db.PostgresRepository
	redisClient redis.RedisClient
}

func NewPaymentService(db *db.PostgresRepository, redisClient redis.RedisClient) PaymentService {
	s := &paymentService{
		db:          db,
		redisClient: redisClient,
	}

	return s
}

func (s *paymentService) CreatePayment(input model.Payment) error {
	if input.Amount <= 0 {
		return fmt.Errorf("amount must be greater than zero")
	}

	payment := model.Payment{
		CorrelationID: input.CorrelationID,
		Amount:        input.Amount,
		RequestedAt:   time.Now().UTC(),
	}

	paymentJSON, err := json.Marshal(payment)
	if err != nil {
		log.Printf("Error marshaling service: %v", err)
		return fmt.Errorf("could not process service: %w", err)
	}

	ctx := context.Background()
	if err := s.redisClient.LPush(ctx, paymentQueueName, paymentJSON); err != nil {
		log.Printf("CRITICAL: Failed to enqueue service to Redis. CorrelationID: %s, Error: %v", payment.CorrelationID, err)
		return fmt.Errorf("erro ao enfileirar pagamento: %w", err)
	}

	log.Printf("Payment enqueued: %s", payment.CorrelationID)
	return nil
}

func (s *paymentService) RetrievePaymentSummary(from, to string) (*model.PaymentSummaryResponse, error) {
	fromTime, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return nil, fmt.Errorf("formato inválido para 'from': %w", err)
	}

	toTime, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return nil, fmt.Errorf("formato inválido para 'to': %w", err)
	}

	ctx := context.Background()
	return s.db.GetPaymentSummary(ctx, fromTime, toTime)
}
