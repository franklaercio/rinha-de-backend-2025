package externalapi

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"rinha-de-backend-2025/core/model"
	"rinha-de-backend-2025/infra/db"
	"sync"
	"time"

	"github.com/bytedance/sonic"
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
	return &client{
		DB:                 db,
		paymentURLDefault:  defaultURL,
		paymentURLFallback: fallbackURL,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
		healthCache: make(map[model.PaymentProcessor]bool),
	}
}

func (c *client) SendPayment(payment model.Payment, origin model.PaymentProcessor) error {
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

	req, err := http.NewRequest("POST", url+"/payments", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("erro ao criar requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("erro na requisição: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: status %d, body: %s", resp.StatusCode, respBody)
	}

	if _, err := c.DB.SavePayment(payment, origin); err != nil {
		log.Printf("[ERROR] ERRO CRÍTICO ao salvar no banco: %v", err)
	}
	//log.Printf("[INFO] Payment %s saved with success", payment.CorrelationID)

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
