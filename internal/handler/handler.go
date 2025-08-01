package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"rinha-de-backend-2025/core/model"
	"rinha-de-backend-2025/core/service"
	"time"
)

type Handler struct {
	paymentService service.PaymentService
}

func NewHandler(paymentService service.PaymentService) *Handler {
	return &Handler{paymentService: paymentService}
}

type CreatePaymentRequest struct {
	CorrelationID string  `json:"correlationId"`
	Amount        float64 `json:"amount"`
}

func (h *Handler) SendPayment(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}

	var req CreatePaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
	}

	input := model.Payment{
		CorrelationID: req.CorrelationID,
		Amount:        req.Amount,
		RequestedAt:   time.Now().UTC(),
	}

	if err := h.paymentService.CreatePayment(input); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) GetSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}

	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	log.Printf("[INFO] GET /payment-summary?from=%s&to=%s", from, to)

	if from == "" || to == "" {

	}

	summary, err := h.paymentService.RetrievePaymentSummary(from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}
