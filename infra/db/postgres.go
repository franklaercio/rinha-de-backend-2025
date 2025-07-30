package db

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"rinha-de-backend-2025/core/model"
	"time"
)

type Payment struct {
	ID            string
	CorrelationID string
	Amount        float64
	Origin        model.PaymentProcessor
	RequestedAt   time.Time
}

type Repository interface {
	SavePayment(p model.Payment, origin model.PaymentProcessor) (string, error)
	GetPaymentSummary(ctx context.Context, from, to time.Time) (*model.PaymentSummaryResponse, error)
}

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(ctx context.Context, dsn string, maxConnections int) (*PostgresRepository, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid db config: %w", err)
	}

	config.MaxConns = int32(maxConnections)

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("could not create pgx pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("could not ping postgres: %w", err)
	}

	return &PostgresRepository{pool: pool}, nil
}

func (r *PostgresRepository) SavePayment(p model.Payment, origin model.PaymentProcessor) (string, error) {
	query := `
    	INSERT INTO payments (correlation_id, amount, origin, requested_at)
    	VALUES ($1, $2, $3, $4)
   `
	_, err := r.pool.Exec(context.Background(), query, p.CorrelationID, p.Amount, origin, p.RequestedAt)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			log.Printf("[INFO] Pagamento com CorrelationID '%s' já existe. Pulando inserção.", p.CorrelationID)
			return p.CorrelationID, nil
		}
		return "", fmt.Errorf("não foi possível salvar o pagamento: %w", err)
	}
	return p.CorrelationID, nil
}

func (r *PostgresRepository) GetPaymentSummary(ctx context.Context, from, to time.Time) (*model.PaymentSummaryResponse, error) {
	query := `
		SELECT origin, COUNT(*) AS total_requests, COALESCE(SUM(amount), 0) AS total_amount
		FROM payments
		WHERE requested_at BETWEEN $1 AND $2
		GROUP BY origin
	`

	rows, err := r.pool.Query(ctx, query, from, to)
	if err != nil {
		return nil, fmt.Errorf("could not get summary: %w", err)
	}
	defer rows.Close()

	summary := model.PaymentSummaryResponse{}

	for rows.Next() {
		var origin int
		var totalRequests int
		var totalAmount float64

		if err := rows.Scan(&origin, &totalRequests, &totalAmount); err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}

		switch model.PaymentProcessor(origin) {
		case model.PaymentDefault:
			summary.Default = model.PaymentSummary{
				TotalRequests: totalRequests,
				TotalAmount:   totalAmount,
			}
		case model.PaymentFallback:
			summary.Fallback = model.PaymentSummary{
				TotalRequests: totalRequests,
				TotalAmount:   totalAmount,
			}
		}
	}

	return &summary, nil
}
