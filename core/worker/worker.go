package worker

import (
	"context"
	"github.com/bytedance/sonic"
	"log"
	"rinha-de-backend-2025/core/model"
	"rinha-de-backend-2025/core/service"
	"rinha-de-backend-2025/infra/db"
	"rinha-de-backend-2025/infra/externalapi"
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
	ExternalApi    externalapi.Client
}

func NewWorker(workerCount int, db *db.PostgresRepository, redis redis.RedisClient, paymentService service.PaymentService, externalapi externalapi.Client) *Worker {
	w := &Worker{
		DB:             db,
		RedisClient:    redis,
		PaymentService: paymentService,
		ExternalApi:    externalapi,
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
	if w.ExternalApi.IsHealthy(model.PaymentDefault) {
		if err := w.ExternalApi.SendPayment(payment, model.PaymentDefault); err == nil {
			//log.Printf("[Worker %d] Payment sent via Default: %s", worker, payment.CorrelationID)
			return
		}
		log.Printf("[Worker %d] Default failed for %s", worker, payment.CorrelationID)
	}

	if w.ExternalApi.IsHealthy(model.PaymentFallback) {
		if err := w.ExternalApi.SendPayment(payment, model.PaymentFallback); err == nil {
			//log.Printf("[Worker %d] Payment sent via Fallback: %s", worker, payment.CorrelationID)
			return
		}
		log.Printf("[Worker %d] Fallback failed for %s", worker, payment.CorrelationID)
	}

	//log.Printf("[Worker %d] Re-enqueueing payment: %s", worker, payment.CorrelationID)
	paymentJSON, _ := sonic.Marshal(payment)
	_ = w.RedisClient.LPush(context.Background(), queueName, paymentJSON)
}
