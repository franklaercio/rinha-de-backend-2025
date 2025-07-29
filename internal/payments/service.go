package payments

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"rinha-de-backend-2025/domain"
	"rinha-de-backend-2025/internal/db"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/redis/go-redis/v9"
)

const (
	paymentQueueName = "payment_queue"
)

type Service interface {
	CreatePayment(input CreatePaymentInput) error
	GetPaymentSummary(from, to string) (*domain.PaymentSummaryResponse, error)
}

type service struct {
	db                      *db.PostgresRepository
	redisClient             *redis.Client
	paymentURLDefault       string
	paymentURLFallback      string
	httpClient              *http.Client
	lastHealthCheckDefault  time.Time
	lastHealthCheckFallback time.Time
	healthCache             map[domain.PaymentProcessor]bool
	mu                      sync.Mutex
}

type CreatePaymentInput struct {
	CorrelationID string  `json:"correlationId"`
	Amount        float64 `json:"amount"`
	RequestedAt   time.Time
}

type HealthCheck struct {
	Failing         bool
	MinResponseTime int64
}

func NewService(workerCount int, db *db.PostgresRepository, redisClient *redis.Client, paymentURLDefault, paymentURLFallback string) Service {
	s := &service{
		db:                 db,
		redisClient:        redisClient,
		paymentURLDefault:  paymentURLDefault,
		paymentURLFallback: paymentURLFallback,
		httpClient:         &http.Client{Timeout: 5 * time.Second},
		healthCache:        make(map[domain.PaymentProcessor]bool),
	}

	if workerCount <= 0 {
		workerCount = 1
	}

	log.Printf("Starting %d payment workers...", workerCount)
	for i := 1; i <= workerCount; i++ {
		go s.startWorker(i)
	}

	return s
}

func (s *service) CreatePayment(input CreatePaymentInput) error {
	if input.Amount <= 0 {
		return fmt.Errorf("amount must be greater than zero")
	}

	payment := domain.Payment{
		CorrelationID: input.CorrelationID,
		Amount:        input.Amount,
		RequestedAt:   time.Now().UTC(),
	}

	paymentJSON, err := sonic.Marshal(payment)
	if err != nil {
		log.Printf("Error marshaling payment: %v", err)
		return fmt.Errorf("could not process payment: %w", err)
	}

	ctx := context.Background()
	if err := s.redisClient.LPush(ctx, paymentQueueName, paymentJSON).Err(); err != nil {
		log.Printf("CRITICAL: Failed to enqueue payment to Redis. CorrelationID: %s, Error: %v", payment.CorrelationID, err)
		return fmt.Errorf("erro ao enfileirar pagamento: %w", err)
	}

	log.Printf("Payment enqueued: %s", payment.CorrelationID)
	return nil
}

func (s *service) startWorker(workerID int) {
	ctx := context.Background()

	for {
		result, err := s.redisClient.BRPop(ctx, 0, paymentQueueName).Result()
		if err != nil {
			log.Printf("[Worker %d] Error popping payment from Redis queue: %v. Retrying in 5 seconds...", workerID, err)
			time.Sleep(5 * time.Second)
			continue
		}

		paymentJSON := result[1]
		var payment domain.Payment
		if err := sonic.Unmarshal([]byte(paymentJSON), &payment); err != nil {
			log.Printf("[Worker %d] Error unmarshaling payment JSON: %v. Discarding message.", workerID, err)
			continue
		}

		log.Printf("[Worker %d] Processing payment: %s", workerID, payment.CorrelationID)
		s.processDequeuedPayment(payment, workerID)
	}
}

func (s *service) processDequeuedPayment(payment domain.Payment, workerID int) {
	if s.isHealthy(domain.PaymentDefault) {
		if err := s.sendToPaymentProcessor(payment, domain.PaymentDefault); err == nil {
			log.Printf("[Worker %d] Payment sent via Default: %s", workerID, payment.CorrelationID)
			return
		}
		log.Printf("[Worker %d] Default failed for %s", workerID, payment.CorrelationID)
	}

	if s.isHealthy(domain.PaymentFallback) {
		if err := s.sendToPaymentProcessor(payment, domain.PaymentFallback); err == nil {
			log.Printf("[Worker %d] Payment sent via Fallback: %s", workerID, payment.CorrelationID)
			return
		}
		log.Printf("[Worker %d] Fallback failed for %s", workerID, payment.CorrelationID)
	}

	log.Printf("[Worker %d] Re-enqueueing payment: %s", workerID, payment.CorrelationID)
	paymentJSON, _ := sonic.Marshal(payment)
	_ = s.redisClient.LPush(context.Background(), paymentQueueName, paymentJSON).Err()
}

func (s *service) sendToPaymentProcessor(p domain.Payment, origin domain.PaymentProcessor) error {
	payload := map[string]interface{}{
		"correlationId": p.CorrelationID,
		"amount":        p.Amount,
		"requestedAt":   p.RequestedAt.Format(time.RFC3339Nano),
	}

	body, err := sonic.Marshal(payload)
	if err != nil {
		return fmt.Errorf("erro ao serializar JSON: %w", err)
	}

	var url string
	switch origin {
	case domain.PaymentDefault:
		url = s.paymentURLDefault
	case domain.PaymentFallback:
		url = s.paymentURLFallback
	}

	req, err := http.NewRequest("POST", url+"/payments", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("erro ao criar requisição: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("erro na requisição: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: status %d, body: %s", resp.StatusCode, respBody)
	}

	_, err = s.db.SavePayment(p, origin)
	if err != nil {
		log.Printf("CRITICAL: Error saving payment to DB after successful processing: %v", err)
		return nil
	}
	log.Printf("Payment saved successfully to DB: %s", p.CorrelationID)

	return nil
}

func (s *service) GetPaymentSummary(from, to string) (*domain.PaymentSummaryResponse, error) {
	fromTime, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return nil, fmt.Errorf("invalid 'from' date format: %w", err)
	}

	toTime, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return nil, fmt.Errorf("invalid 'to' date format: %w", err)
	}

	ctx := context.Background()
	return s.db.GetPaymentSummary(ctx, fromTime, toTime)
}

func (s *service) GetHealthCheck(origin domain.PaymentProcessor) error {
	var url string
	switch origin {
	case domain.PaymentDefault:
		url = s.paymentURLDefault
	case domain.PaymentFallback:
		url = s.paymentURLFallback
	}

	req, err := http.NewRequest("GET", url+"/payments/service-health", nil)
	if err != nil {
		return fmt.Errorf("erro ao criar requisição: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("erro na requisição: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: status %d, body: %s", resp.StatusCode, respBody)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("erro ao ler resposta: %w", err)
	}

	var health HealthCheck
	err = sonic.Unmarshal(respBody, &health)
	if err != nil {
		return fmt.Errorf("erro ao decodificar resposta: %w", err)
	}

	if health.Failing {
		return fmt.Errorf("API %s is unhealthy", url)
	}

	return nil
}

func (s *service) isHealthy(origin domain.PaymentProcessor) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	var lastCheck *time.Time
	switch origin {
	case domain.PaymentDefault:
		lastCheck = &s.lastHealthCheckDefault
	case domain.PaymentFallback:
		lastCheck = &s.lastHealthCheckFallback
	}

	now := time.Now()
	if now.Sub(*lastCheck) < 5*time.Second {
		return s.healthCache[origin]
	}

	err := s.GetHealthCheck(origin)
	*lastCheck = now
	s.healthCache[origin] = (err == nil)

	return s.healthCache[origin]
}
