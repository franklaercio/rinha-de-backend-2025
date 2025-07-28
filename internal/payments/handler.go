package payments

import (
	"io"
	"log"
	"net/http"
	"time"

	"github.com/bytedance/sonic"
	"github.com/labstack/echo/v4"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
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

	input := CreatePaymentInput{
		CorrelationID: req.CorrelationID,
		Amount:        req.Amount,
		RequestedAt:   time.Now().UTC(),
	}

	if err := h.service.CreatePayment(input); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, map[string]string{"message": "payment created"})
}

func (h *Handler) GetSummary(c echo.Context) error {
	from := c.QueryParam("from")
	to := c.QueryParam("to")

	log.Printf("[INFO] GET /payments-summary?from=%s&to=%s", from, to)

	if from == "" || to == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "'from' and 'to' query params required"})
	}

	summary, err := h.service.GetPaymentSummary(from, to)
	if err != nil {
		log.Printf("[ERROR] Error getting payment summary: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	respBytes, err := sonic.Marshal(summary)
	if err != nil {
		log.Printf("[ERROR] Error encoding response: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to encode response"})
	}

	return c.Blob(http.StatusOK, echo.MIMEApplicationJSON, respBytes)
}
