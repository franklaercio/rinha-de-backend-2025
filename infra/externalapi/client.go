package externalapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"rinha-de-backend-2025/core/model"
	"rinha-de-backend-2025/infra/db"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/sony/gobreaker"
)

type Client interface {
	SendPayment(p model.Payment, origin model.PaymentProcessor) error
	IsHealthy(origin model.PaymentProcessor) bool
}

type client struct {
	DB                      *db.PostgresRepository
	paymentURLDefault       string
	paymentURLFallback      string
	httpClient              *http.Client
	lastHealthCheckDefault  time.Time
	lastHealthCheckFallback time.Time
	healthCache             map[model.PaymentProcessor]bool
	mu                      sync.Mutex
	cbDefault               *gobreaker.CircuitBreaker
	cbFallback              *gobreaker.CircuitBreaker
}

type HealthCheck struct {
	Failing         bool
	MinResponseTime int64
}

func NewClient(
	db *db.PostgresRepository,
	defaultURL string,
	fallbackURL string,
) Client {
	settings := gobreaker.Settings{
		Name:        "ExternalPaymentService",
		MaxRequests: 10,
		Interval:    2 * time.Second,
		Timeout:     4 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures > 10
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			log.Printf("Circuit Breaker '%s' changed state from '%s' to '%s'", name, from, to)
		},
	}

	return &client{
		DB:                 db,
		paymentURLDefault:  defaultURL,
		paymentURLFallback: fallbackURL,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
		healthCache: make(map[model.PaymentProcessor]bool),
		cbDefault:   gobreaker.NewCircuitBreaker(settings),
		cbFallback:  gobreaker.NewCircuitBreaker(settings),
	}
}

func (c *client) SendPayment(payment model.Payment, origin model.PaymentProcessor) error {
	if _, err := c.DB.SavePayment(payment, origin); err != nil {
		log.Printf("[ERROR] Falha ao salvar o pagamento localmente para CorrelationID '%s' antes de enviar para a API externa: %v", payment.CorrelationID, err)
	}

	payload := map[string]interface{}{
		"correlationId": payment.CorrelationID,
		"amount":        payment.Amount,
		"requestedAt":   payment.RequestedAt.Format(time.RFC3339Nano),
	}

	body, err := sonic.Marshal(payload)
	if err != nil {
		return fmt.Errorf("erro ao serializar JSON: %w", err)
	}

	url := c.resolveURL(origin)
	cb := c.cbDefault
	if origin == model.PaymentFallback {
		cb = c.cbFallback
	}

	_, err = cb.Execute(func() (interface{}, error) {
		req, err := http.NewRequestWithContext(context.Background(), "POST", url+"/payments", bytes.NewBuffer(body))
		if err != nil {
			return nil, fmt.Errorf("erro ao criar requisição: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if err == context.Canceled || err == context.DeadlineExceeded {
				return nil, err
			}
			return nil, fmt.Errorf("erro na requisição para API externa: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 300 {
			respBody, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("erro na API externa: status %d, corpo: %s", resp.StatusCode, respBody)
		}

		return nil, nil
	})

	if err != nil {
		if err == gobreaker.ErrOpenState {
			log.Printf("[Circuit Breaker] A API %s está em estado OPEN. Não enviando pagamento %s.", origin, payment.CorrelationID)
			return fmt.Errorf("circuit breaker para %s está aberto", origin)
		}
		return err
	}

	log.Printf("[INFO] Pagamento salvo com sucesso: %s", payment.CorrelationID)

	return nil
}

func (c *client) getHealthCheck(origin model.PaymentProcessor) error {
	url := c.resolveURL(origin)

	req, err := http.NewRequest("GET", url+"/payments/service-health", nil)
	if err != nil {
		return fmt.Errorf("erro ao criar requisição: %w", err)
	}

	resp, err := c.httpClient.Do(req)
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
	if err := sonic.Unmarshal(respBody, &health); err != nil {
		return fmt.Errorf("erro ao decodificar resposta: %w", err)
	}

	if health.Failing {
		return fmt.Errorf("API %s está fora do ar", url)
	}

	return nil
}

func (c *client) IsHealthy(origin model.PaymentProcessor) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	var lastCheck *time.Time
	switch origin {
	case model.PaymentDefault:
		lastCheck = &c.lastHealthCheckDefault
	case model.PaymentFallback:
		lastCheck = &c.lastHealthCheckFallback
	}

	now := time.Now()
	if now.Sub(*lastCheck) < 5*time.Second {
		return c.healthCache[origin]
	}

	err := c.getHealthCheck(origin)
	*lastCheck = now
	c.healthCache[origin] = (err == nil)

	return c.healthCache[origin]
}

func (c *client) resolveURL(origin model.PaymentProcessor) string {
	switch origin {
	case model.PaymentDefault:
		return c.paymentURLDefault
	case model.PaymentFallback:
		return c.paymentURLFallback
	default:
		return ""
	}
}
