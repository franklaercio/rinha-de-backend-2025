package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/rabbitmq/amqp091-go"
	"io"
	"log"
	"net/http"
	"rinha-de-backend-2025/domain"
	"rinha-de-backend-2025/internal/broker"
	"rinha-de-backend-2025/internal/db"
	"time"
)

type Service interface {
	CreatePayment(input CreatePaymentInput) error
	GetPaymentSummary(from, to string) (*domain.PaymentSummaryResponse, error)
}

type service struct {
	db                 *db.PostgresRepository
	rabbitChannel      *amqp091.Channel
	paymentURLDefault  string
	paymentURLFallback string
	httpClient         *http.Client
}

type CreatePaymentInput struct {
	CorrelationID string  `json:"correlationId"`
	Amount        float64 `json:"amount"`
}

func NewService(workerCount int, db *db.PostgresRepository, rabbitChannel *amqp091.Channel, paymentURLDefault, paymentURLFallback string) Service {
	s := &service{
		db:                 db,
		rabbitChannel:      rabbitChannel,
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
		log.Printf("Error marshaling payment: %v", err)
		return fmt.Errorf("could not process payment: %w", err)
	}

	ctx := context.Background()

	err = s.rabbitChannel.PublishWithContext(ctx,
		"",
		broker.PaymentQueueName,
		false,
		false,
		amqp091.Publishing{
			ContentType:  "application/json",
			Body:         paymentJSON,
			DeliveryMode: amqp091.Persistent,
		})

	if err != nil {
		log.Printf("CRITICAL: Failed to publish payment to RabbitMQ. CorrelationID: %s, Error: %v", payment.CorrelationID, err)
		return fmt.Errorf("erro ao enfileirar pagamento: %w", err)
	}

	log.Printf("Payment enqueued to RabbitMQ: %s", payment.CorrelationID)
	return nil
}

func (s *service) startWorker(workerID int) {
	log.Printf("Payment worker #%d started...", workerID)

	msgs, err := s.rabbitChannel.Consume(
		broker.PaymentQueueName,
		fmt.Sprintf("worker-%d", workerID),
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Worker #%d failed to register a consumer: %v", workerID, err)
	}

	for d := range msgs {
		var payment domain.Payment
		if err := json.Unmarshal(d.Body, &payment); err != nil {
			log.Printf("[Worker %d] Error unmarshaling payment JSON: %v. Discarding message.", workerID, err)
			d.Nack(false, false) // Rejeita a mensagem, não re-enfileira.
			continue
		}

		log.Printf("[Worker %d] Processing payment: %s", workerID, payment.CorrelationID)
		s.processDequeuedPayment(payment, workerID)

		d.Ack(false)
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

		log.Printf("[Worker %d] Error sending payment %s to Default (attempt %d/%d): %v", workerID, payment.CorrelationID, retries+1, maxRetries, err)
		retries++

		if retries >= maxRetries {
			log.Printf("[Worker %d] Max retries reached for Default. Trying Fallback for payment %s", workerID, payment.CorrelationID)

			err := s.sendToPaymentProcessor(payment, domain.PaymentFallback)
			if err != nil {
				log.Printf("[Worker %d] FATAL: Payment failed on Fallback as well for %s: %v", workerID, payment.CorrelationID, err)
			} else {
				log.Printf("[Worker %d] Payment sent successfully to Fallback: %s", workerID, payment.CorrelationID)
			}
			return
		}

		backoff := time.Duration(retries*2) * time.Second
		log.Printf("[Worker %d] Retrying payment %s in %v", workerID, payment.CorrelationID, backoff)
		time.Sleep(backoff)
	}
}

func (s *service) sendToPaymentProcessor(p domain.Payment, origin domain.PaymentProcessor) error {
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
		return fmt.Errorf("API error: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	_, err = s.db.SavePayment(p, origin)
	if err != nil {
		log.Printf("CRITICAL: Error saving payment to DB after successful processing. The message will be ACK'd to prevent double payment. CorrelationID: %s, Error: %v", p.CorrelationID, err)
		return nil
	}

	log.Printf("Payment saved successfully to DB: %s", p.CorrelationID)
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
