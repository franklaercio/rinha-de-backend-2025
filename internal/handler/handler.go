package handler

import (
	"io"
	"log"
	"net/http"
	"rinha-de-backend-2025/core/model"
	"rinha-de-backend-2025/core/service"
	"time"

	"github.com/bytedance/sonic"
	"github.com/labstack/echo/v4"
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

func (h *Handler) SendPayment(c echo.Context) error {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
	}

	var req CreatePaymentRequest
	if err := sonic.Unmarshal(body, &req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}

	input := model.Payment{
		CorrelationID: req.CorrelationID,
		Amount:        req.Amount,
		RequestedAt:   time.Now().UTC(),
	}

	if err := h.paymentService.CreatePayment(input); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, map[string]string{"message": "service created"})
}

func (h *Handler) GetSummary(c echo.Context) error {
	from := c.QueryParam("from")
	to := c.QueryParam("to")

	log.Printf("[INFO] GET /service-summary?from=%s&to=%s", from, to)

	if from == "" || to == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "'from' and 'to' query params required"})
	}

	summary, err := h.paymentService.RetrievePaymentSummary(from, to)
	if err != nil {
		log.Printf("[ERROR] Error getting service summary: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	respBytes, err := sonic.Marshal(summary)
	if err != nil {
		log.Printf("[ERROR] Error encoding response: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to encode response"})
	}

	return c.Blob(http.StatusOK, echo.MIMEApplicationJSON, respBytes)
}
