package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"rinha-de-backend-2025/domain"
	"rinha-de-backend-2025/internal/db"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	paymentQueueName = "payment_queue"
)

type Service interface {
	CreatePayment(input CreatePaymentInput) error
	GetPaymentSummary(from, to string) (*domain.PaymentSummaryResponse, error)
}

// --- REFINADO ---
// Removidos os campos não utilizados: queue, mu, wg.
type service struct {
	db                 *db.RedisRepository
	redisClient        *redis.Client
	paymentURLDefault  string
	paymentURLFallback string
	httpClient         *http.Client
}

type CreatePaymentInput struct {
	CorrelationID string  `json:"correlationId"`
	Amount        float64 `json:"amount"`
}

func NewService(workerCount int, db *db.RedisRepository, redisClient *redis.Client, paymentURLDefault, paymentURLFallback string) Service {
	s := &service{
		db:                 db,
		redisClient:        redisClient,
		paymentURLDefault:  paymentURLDefault,
		paymentURLFallback: paymentURLFallback,
		httpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        500,
				MaxIdleConnsPerHost: 500,
				IdleConnTimeout:     90 * time.Second,
				DisableKeepAlives:   false,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
			Timeout: 10 * time.Second,
		},
	}

	log.Printf("Starting %d payment workers...", workerCount)
	for i := 1; i <= workerCount; i++ {
		go s.startWorker(i) // Passa um ID para cada worker
	}

	return s
}

func (s *service) CreatePayment(input CreatePaymentInput) error {
	// ... (this function is correct and remains unchanged)
	if input.Amount <= 0 {
		return fmt.Errorf("amount must be greater than zero")
	}

	payment := domain.Payment{
		CorrelationID: input.CorrelationID,
		Amount:        input.Amount,
		RequestedAt:   time.Now().UTC(),
	}

	paymentJSON, err := json.Marshal(payment)
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
	log.Printf("Payment worker #%d started...", workerID)
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
		if err := json.Unmarshal([]byte(paymentJSON), &payment); err != nil {
			log.Printf("[Worker %d] Error unmarshaling payment JSON: %v. Discarding message.", workerID, err)
			continue
		}

		log.Printf("[Worker %d] Processing payment: %s", workerID, payment.CorrelationID)
		s.processPaymentWithRetries(payment, workerID)
	}
}

func (s *service) processPaymentWithRetries(payment domain.Payment, workerID int) {
	// Phase 1: Try to process the payment with the Default processor.
	err := s.tryProcessor(payment, domain.PaymentDefault, workerID)

	// Phase 2: If Default fails, try the Fallback processor.
	if err != nil {
		log.Printf("[Worker %d] Default processor failed for %s after all retries. Trying Fallback.", workerID, payment.CorrelationID)
		err = s.tryProcessor(payment, domain.PaymentFallback, workerID)
	}

	// Phase 3: If both processors fail, log a fatal error for this payment.
	if err != nil {
		log.Printf("[Worker %d] FATAL: Payment %s failed on both Default and Fallback processors. Error: %v. Discarding message.", workerID, payment.CorrelationID, err)
	}
}

func (s *service) tryProcessor(p domain.Payment, origin domain.PaymentProcessor, workerID int) error {
	// --- Step 1: Call the external API with its own retry logic ---
	apiRetries := 0
	maxApiRetries := 5
	for {
		err := s.callExternalAPI(p, origin)
		if err == nil {
			// External API call was successful, break the loop to proceed to DB save.
			break
		}

		log.Printf("[Worker %d] Error calling processor %s for payment %s: %v", workerID, origin, p.CorrelationID, err)
		apiRetries++
		if apiRetries > maxApiRetries {
			return fmt.Errorf("max retries reached for external API at %s", origin)
		}

		backoff := time.Duration(apiRetries*2) * time.Second
		log.Printf("[Worker %d] Retrying API call for %s in %v", workerID, p.CorrelationID, backoff)
		time.Sleep(backoff)
	}

	// --- Step 2: Persist the result to our internal DB with a robust retry loop ---
	// This loop MUST eventually succeed to maintain consistency.
	for {
		_, err := s.db.SavePayment(p, origin)
		if err == nil {
			log.Printf("[Worker %d] Payment %s processed via %s and saved to DB.", workerID, p.CorrelationID, origin)
			return nil // The entire operation for this processor is successful.
		}

		// If the DB save fails, we are in an inconsistent state.
		// We MUST retry until it succeeds.
		log.Printf("[Worker %d] CRITICAL: Failed to save processed payment %s to DB. Error: %v. Retrying in 3s...", workerID, p.CorrelationID, err)
		time.Sleep(3 * time.Second)
	}
}

func (s *service) callExternalAPI(p domain.Payment, origin domain.PaymentProcessor) error {
	payload := map[string]interface{}{
		"correlationId": p.CorrelationID,
		"amount":        p.Amount,
		"requestedAt":   p.RequestedAt.Format(time.RFC3339Nano),
	}

	body, err := json.Marshal(payload)
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

	return nil
}

func (s *service) GetPaymentSummary(from, to string) (*domain.PaymentSummaryResponse, error) {
	// ... (this function is correct and remains unchanged)
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
