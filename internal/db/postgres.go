package db

import (
	"context"
	"database/sql"
	"fmt"
	"rinha-de-backend-2025/domain"
	"time"
)

type Payment struct {
	ID            string
	CorrelationID string
	Amount        float64
	Origin        domain.PaymentProcessor
	RequestedAt   time.Time
}

type Repository interface {
	SavePayment(p domain.Payment, origin domain.PaymentProcessor) (string, error)
	GetSummary() (*domain.PaymentSummary, error)
}

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(dsn string) (*PostgresRepository, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("could not connect to postgres: %w", err)
	}

	db.SetMaxOpenConns(180)
	db.SetMaxIdleConns(180)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(1 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("could not ping postgres: %w", err)
	}

	return &PostgresRepository{db: db}, nil
}

func (r *PostgresRepository) SavePayment(p domain.Payment, origin domain.PaymentProcessor) (string, error) {
	query := `
    	INSERT INTO payments (correlation_id, amount, origin, requested_at)
    	VALUES ($1, $2, $3, $4)
   `

	_, err := r.db.Exec(query, p.CorrelationID, p.Amount, origin, p.RequestedAt)
	if err != nil {
		return "", fmt.Errorf("could not save payment: %w", err)
	}
	return p.CorrelationID, nil
}

func (r *PostgresRepository) GetPaymentSummary(ctx context.Context, from, to time.Time) (*domain.PaymentSummaryResponse, error) {
	query := `
		SELECT origin, COUNT(*) AS total_requests, COALESCE(SUM(amount), 0) AS total_amount
		FROM payments
		WHERE requested_at BETWEEN $1 AND $2
		GROUP BY origin
	`

	rows, err := r.db.QueryContext(ctx, query, from, to)
	if err != nil {
		return nil, fmt.Errorf("could not get summary: %w", err)
	}
	defer rows.Close()

	summary := domain.PaymentSummaryResponse{}

	for rows.Next() {
		var origin int
		var totalRequests int
		var totalAmount float64

		if err := rows.Scan(&origin, &totalRequests, &totalAmount); err != nil {
			return nil, fmt.Errorf("could not get summary: %w", err)
		}

		switch domain.PaymentProcessor(origin) {
		case domain.PaymentDefault:
			summary.Default = domain.PaymentSummary{
				TotalRequests: totalRequests,
				TotalAmount:   totalAmount,
			}
		case domain.PaymentFallback:
			summary.Fallback = domain.PaymentSummary{
				TotalRequests: totalRequests,
				TotalAmount:   totalAmount,
			}
		}
	}

	return &summary, nil
}
