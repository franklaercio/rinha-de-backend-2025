package domain

type PaymentProcessor int

const (
	PaymentDefault PaymentProcessor = iota
	PaymentFallback
)
