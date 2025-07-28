package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/nats-io/nats.go"
	"io"
	"log"
	"net/http"
	"rinha-de-backend-2025/domain"
	"rinha-de-backend-2025/internal/db"
	"time"
)

const PaymentSubject = "payments.created"

type Service interface {
	CreatePayment(input CreatePaymentInput) error
	GetPaymentSummary(from, to string) (*domain.PaymentSummaryResponse, error)
}

type service struct {
	db                 *db.PostgresRepository
	natsConn           *nats.Conn
	paymentURLDefault  string
	paymentURLFallback string
	httpClient         *http.Client
}

type CreatePaymentInput struct {
	CorrelationID string  `json:"correlationId"`
	Amount        float64 `json:"amount"`
}

func NewService(workerCount int, db *db.PostgresRepository, natsConn *nats.Conn, paymentURLDefault, paymentURLFallback string) Service {
	s := &service{
		db:                 db,
		natsConn:           natsConn,
		paymentURLDefault:  paymentURLDefault,
		paymentURLFallback: paymentURLFallback,
		httpClient:         &http.Client{Timeout: 5 * time.Second},
	}

	for i := 0; i < workerCount; i++ {
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

	paymentJSON, err := json.Marshal(payment)
	if err != nil {
		return fmt.Errorf("could not marshal payment: %w", err)
	}

	err = s.natsConn.Publish(PaymentSubject, paymentJSON)
	if err != nil {
		return fmt.Errorf("failed to publish to NATS: %w", err)
	}

	log.Printf("Payment published to NATS: %s", payment.CorrelationID)
	return nil
}

func (s *service) startWorker(workerID int) {
	log.Printf("Worker #%d subscribed to NATS subject '%s'", workerID, PaymentSubject)

	_, err := s.natsConn.Subscribe(PaymentSubject, func(msg *nats.Msg) {
		var payment domain.Payment
		if err := json.Unmarshal(msg.Data, &payment); err != nil {
			log.Printf("[Worker %d] Error decoding message: %v", workerID, err)
			return
		}

		log.Printf("[Worker %d] Processing payment: %s", workerID, payment.CorrelationID)
		s.processDequeuedPayment(payment, workerID)
	})

	if err != nil {
		log.Fatalf("Worker #%d failed to subscribe: %v", workerID, err)
	}
}

func (s *service) processDequeuedPayment(payment domain.Payment, workerID int) {
	retries := 0
	const maxRetries = 5

	for {
		err := s.sendToPaymentProcessor(payment, domain.PaymentDefault)
		if err == nil {
			log.Printf("[Worker %d] Payment sent successfully to Default: %s", workerID, payment.CorrelationID)
			return
		}

		log.Printf("[Worker %d] Failed to send to Default (try %d/%d): %v", workerID, retries+1, maxRetries, err)
		retries++

		if retries >= maxRetries {
			log.Printf("[Worker %d] Trying Fallback for %s", workerID, payment.CorrelationID)

			err := s.sendToPaymentProcessor(payment, domain.PaymentFallback)
			if err != nil {
				log.Printf("[Worker %d] FATAL: Fallback failed for %s: %v", workerID, payment.CorrelationID, err)
			} else {
				log.Printf("[Worker %d] Payment sent to Fallback: %s", workerID, payment.CorrelationID)
			}
			return
		}

		backoff := time.Duration(retries*1) * time.Second
		log.Printf("[Worker %d] Retrying in %v", workerID, backoff)
		time.Sleep(backoff)
	}
}

func (s *service) sendToPaymentProcessor(p domain.Payment, origin domain.PaymentProcessor) error {
	//existPayment, err := s.db.ExistsPaymentByCorrelationID(context.Background(), p.CorrelationID)
	//if existPayment || err != nil {
	//	return fmt.Errorf("payment exist %s", p.CorrelationID)
	//}

	payload := map[string]interface{}{
		"correlationId": p.CorrelationID,
		"amount":        p.Amount,
		"requestedAt":   p.RequestedAt.Format(time.RFC3339Nano),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("serialization error: %w", err)
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
		return fmt.Errorf("request creation error: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("api error %d: %s", resp.StatusCode, string(respBody))
	}

	_, err = s.db.SavePayment(p, origin)
	if err != nil {
		log.Printf("Error saving to DB. CorrelationID: %s. Error: %v", p.CorrelationID, err)
		return nil
	}

	log.Printf("Payment saved to DB: %s", p.CorrelationID)
	return nil
}

func (s *service) GetPaymentSummary(from, to string) (*domain.PaymentSummaryResponse, error) {
	fromTime, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return nil, err
	}

	toTime, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	return s.db.GetPaymentSummary(ctx, fromTime, toTime)
}
