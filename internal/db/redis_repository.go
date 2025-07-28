package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"rinha-de-backend-2025/domain"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisRepository struct {
	client *redis.Client
}

func NewRedisRepository(client *redis.Client) *RedisRepository {
	return &RedisRepository{client: client}
}

func (r *RedisRepository) SavePayment(p domain.Payment, origin domain.PaymentProcessor) (string, error) {
	ctx := context.Background()
	pipe := r.client.Pipeline()

	paymentKey := fmt.Sprintf("payment:%s", p.CorrelationID)
	paymentData, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("could not marshal payment: %w", err)
	}
	pipe.HSet(ctx, paymentKey, "data", paymentData)
	pipe.HSet(ctx, paymentKey, "origin", int(origin))

	dateStr := p.RequestedAt.Format("2006-01-02")
	summaryKey := fmt.Sprintf("summary:%d:%s", origin, dateStr)

	pipe.HIncrBy(ctx, summaryKey, "total_requests", 1)
	pipe.HIncrByFloat(ctx, summaryKey, "total_amount", p.Amount)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return "", fmt.Errorf("could not execute redis pipeline for save: %w", err)
	}

	return p.CorrelationID, nil
}

func (r *RedisRepository) GetPaymentSummary(ctx context.Context, from, to time.Time) (*domain.PaymentSummaryResponse, error) {
	summary := &domain.PaymentSummaryResponse{}
	origins := []domain.PaymentProcessor{domain.PaymentDefault, domain.PaymentFallback}

	for _, origin := range origins {
		var totalRequests int
		var totalAmount float64

		for date := from; !date.After(to); date = date.AddDate(0, 0, 1) {
			summaryKey := fmt.Sprintf("summary:%d:%s", origin, date.Format("2006-01-02"))

			data, err := r.client.HGetAll(ctx, summaryKey).Result()
			if err != nil {
				if errors.Is(err, redis.Nil) {
					continue
				}
				return nil, fmt.Errorf("could not get summary for origin %d on %s: %w", origin, date.Format("2006-01-02"), err)
			}

			req, _ := strconv.Atoi(data["total_requests"])
			amt, _ := strconv.ParseFloat(data["total_amount"], 64)

			totalRequests += req
			totalAmount += amt
		}

		paymentSummary := domain.PaymentSummary{
			TotalRequests: totalRequests,
			TotalAmount:   math.Round(totalAmount*10) / 10, // arredonda para 1 casa decimal
		}

		if origin == domain.PaymentDefault {
			summary.Default = paymentSummary
		} else {
			summary.Fallback = paymentSummary
		}
	}

	return summary, nil
}
