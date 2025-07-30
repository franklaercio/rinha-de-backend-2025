package worker

import (
	"context"
	"github.com/bytedance/sonic"
	"log"
	"rinha-de-backend-2025/core/model"
	"rinha-de-backend-2025/core/service"
	"rinha-de-backend-2025/infra/db"
	"rinha-de-backend-2025/infra/externalapi" // Adicionar import
	"rinha-de-backend-2025/infra/redis"
	"time"
)

const (
	queueName = "payment_queue"
)

type Worker struct {
	DB             *db.PostgresRepository
	RedisClient    redis.RedisClient
	PaymentService service.PaymentService
	ExternalApi    externalapi.Client // Tipo agora é externalapi.Client
}

func NewWorker(workerCount int, db *db.PostgresRepository, redis redis.RedisClient, paymentService service.PaymentService, externalapiClient externalapi.Client) *Worker { // Renomear parâmetro
	w := &Worker{
		DB:             db,
		RedisClient:    redis,
		PaymentService: paymentService,
		ExternalApi:    externalapiClient, // Usar o novo nome
	}

	if workerCount <= 0 {
		workerCount = 1
	}

	for i := 1; i <= workerCount; i++ {
		go w.Start(i)
	}

	return w
}

func (w *Worker) Start(worker int) {
	ctx := context.Background()

	for {
		result, err := w.RedisClient.BRPop(ctx, 0, queueName)
		if err != nil {
			log.Printf("[Worker %d] Erro no BRPop: %v. Retry em 5s...", worker, err)
			time.Sleep(5 * time.Second)
			continue
		}

		paymentJSON := result[1]
		var payment model.Payment
		if err := sonic.Unmarshal([]byte(paymentJSON), &payment); err != nil {
			log.Printf("[Worker %d] JSON inválido: %v. Ignorando.", worker, err)
			continue
		}

		//log.Printf("[Worker %d] Processando pagamento: %s", worker, payment.CorrelationID)
		w.process(worker, payment, queueName)
	}
}

func (w *Worker) process(worker int, payment model.Payment, queueName string) {
	errDefault := w.ExternalApi.SendPayment(payment, model.PaymentDefault)
	if errDefault == nil {
		//log.Printf("[Worker %d] Payment sent via Default: %s", worker, payment.CorrelationID)
		return
	}

	//// Se o erro for do Circuit Breaker, não tentamos o fallback imediatamente se o CB estiver aberto
	//if errDefault == gobreaker.ErrOpenState {
	//	log.Printf("[Worker %d] Circuit Breaker do Default está aberto para %s. Tentando Fallback...", worker, payment.CorrelationID)
	//	// Continua para tentar o fallback
	//} else {
	//	log.Printf("[Worker %d] Default failed for %s: %v", worker, payment.CorrelationID, errDefault)
	//}

	// Tenta o Fallback
	errFallback := w.ExternalApi.SendPayment(payment, model.PaymentFallback)
	if errFallback == nil {
		//log.Printf("[Worker %d] Payment sent via Fallback: %s", worker, payment.CorrelationID)
		return
	}

	//if errFallback == gobreaker.ErrOpenState {
	//	log.Printf("[Worker %d] Circuit Breaker do Fallback também está aberto para %s. Re-enfileirando...", worker, payment.CorrelationID)
	//} else {
	//	log.Printf("[Worker %d] Fallback failed for %s: %v", worker, payment.CorrelationID, errFallback)
	//}

	// Ambos falharam ou os Circuit Breakers estão abertos, re-enfileira
	//log.Printf("[Worker %d] Re-enqueueing payment: %s", worker, payment.CorrelationID)
	paymentJSON, _ := sonic.Marshal(payment)                              // Erro ignorado intencionalmente para não bloquear o re-enfileiramento
	_ = w.RedisClient.LPush(context.Background(), queueName, paymentJSON) // Erro ignorado intencionalmente
}
