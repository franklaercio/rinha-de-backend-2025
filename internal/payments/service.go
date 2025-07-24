package payments

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type Service interface {
	CreatePayment(input CreatePaymentInput) error
}

type service struct {
	apiURL     string
	httpClient *http.Client
	queue      chan Payment
}

type CreatePaymentInput struct {
	CorrelationID string  `json:"correlationId"`
	Amount        float64 `json:"amount"`
}

func NewService(apiURL string) Service {
	s := &service{
		apiURL:     apiURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		queue:      make(chan Payment, 5000),
	}

	go s.startWorker()

	return s
}

func (s *service) CreatePayment(input CreatePaymentInput) error {
	if input.Amount <= 0 {
		return fmt.Errorf("amount must be greater than zero")
	}

	payment := Payment{
		CorrelationID: input.CorrelationID,
		Amount:        input.Amount,
		RequestedAt:   time.Now().UTC(),
	}

	select {
	case s.queue <- payment:
		log.Printf("Pagamento enfileirado: %s", payment.CorrelationID)
	default:
		log.Printf("Fila cheia, pagamento descartado: %s", payment.CorrelationID)
	}

	return nil
}

func (s *service) startWorker() {
	for payment := range s.queue {
		retries := 0
		for {
			err := s.sendToPaymentProcessorDefault(payment)
			if err == nil {
				log.Printf("Pagamento enviado com sucesso: %s", payment.CorrelationID)
				break
			}

			log.Printf("Erro ao enviar pagamento %s: %v", payment.CorrelationID, err)

			retries++
			if retries > 5 {
				log.Printf("Falha permanente após %d tentativas: %s", retries, payment.CorrelationID)
				break
			}

			backoff := time.Duration(retries*2) * time.Second
			time.Sleep(backoff)
		}
	}
}

func (s *service) sendToPaymentProcessorDefault(p Payment) error {
	payload := map[string]interface{}{
		"correlationId": p.CorrelationID,
		"amount":        p.Amount,
		"requestedAt":   p.RequestedAt.Format(time.RFC3339Nano),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("erro ao serializar JSON: %w", err)
	}

	req, err := http.NewRequest("POST", s.apiURL+"/payments", bytes.NewBuffer(body))
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

	return nil
}
