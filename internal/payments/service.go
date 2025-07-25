package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"rinha-de-backend-2025/domain"
	"rinha-de-backend-2025/internal/db"
	"sync"
	"time"
)

type Service interface {
	CreatePayment(input CreatePaymentInput) error
	GetPaymentSummary(from, to string) (*domain.PaymentSummaryResponse, error)
}

type service struct {
	db                 *db.PostgresRepository
	paymentURLDefault  string
	paymentURLFallback string
	httpClient         *http.Client
	queue              chan domain.Payment
	mu                 sync.Mutex
	wg                 sync.WaitGroup
}

type CreatePaymentInput struct {
	CorrelationID string  `json:"correlationId"`
	Amount        float64 `json:"amount"`
}

func NewService(db db.PostgresRepository, paymentURLDefault, paymentURLFallback string) Service {
	s := &service{
		db:                 &db,
		paymentURLDefault:  paymentURLDefault,
		paymentURLFallback: paymentURLFallback,
		httpClient:         &http.Client{Timeout: 5 * time.Second},
		queue:              make(chan domain.Payment, 10000),
	}

	go s.startWorker()

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

	select {
	case s.queue <- payment:
		log.Printf("Pagamento enfileirado: %s", payment.CorrelationID)
	default:
		log.Printf("Fila cheia, enviando para o Fallback: %s", payment.CorrelationID)
		err := s.sendToPaymentProcessor(payment, domain.PaymentFallback)
		if err == nil {
			log.Printf("Pagamento enviado com sucesso: %s", payment.CorrelationID)
			break
		}
	}

	return nil
}

func (s *service) startWorker() {
	for payment := range s.queue {
		retries := 0
		for {
			err := s.sendToPaymentProcessor(payment, domain.PaymentDefault)
			if err == nil {
				log.Printf("Pagamento enviado com sucesso: %s", payment.CorrelationID)
				break
			}

			log.Printf("Erro ao enviar pagamento %s: %v", payment.CorrelationID, err)

			retries++
			if retries > 5 {
				err := s.sendToPaymentProcessor(payment, domain.PaymentFallback)
				if err != nil {
					log.Printf("Pagamento com falha no Fallback: %v", err)
					log.Printf("Falha permanente após %d tentativas: %s", retries, payment.CorrelationID)
				}
				log.Printf("Payment send with success to Fallback: %s", payment.CorrelationID)
				break
			}

			backoff := time.Duration(retries*2) * time.Second
			time.Sleep(backoff)
		}
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
		return fmt.Errorf("API error: %s", respBody)
	}

	go func(p domain.Payment) {
		s.wg.Add(1)
		defer s.wg.Done()

		pdb, err := s.db.SavePayment(p, origin)
		if err != nil {
			log.Printf("erro ao salvar pagamento async: %v", err)
			return
		}
		log.Printf("Pagamento salvo com sucesso: %s", pdb)
	}(p)

	return nil
}

func (s *service) flushQueue() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for {
		select {
		case p := <-s.queue:
			_, err := s.db.SavePayment(p, domain.PaymentDefault)
			if err != nil {
				log.Printf("Erro no flush: %v", err)
			}
		default:
			return
		}
	}
}

func (s *service) GetPaymentSummary(from, to string) (*domain.PaymentSummaryResponse, error) {
	s.flushQueue()
	s.wg.Wait()

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
