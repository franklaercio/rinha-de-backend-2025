package payments

import "time"

type Payment struct {
	ID            string    `json:"id"`
	CorrelationID string    `json:"correlationId"`
	Amount        float64   `json:"amount"`
	RequestedAt   time.Time `json:"requestedAt"`
}
