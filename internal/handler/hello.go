package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"rinha-de-backend-2025/internal/service"
)

type HelloRequest struct {
	Name string `json:"name"`
}

type HelloResponse struct {
	Message string `json:"message"`
}

func HelloHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Unsupported method", http.StatusMethodNotAllowed)
		return
	}

	var req HelloRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("Request: %s %s %+v", r.Method, r.URL.Path, req)

	resp := HelloResponse{
		Message: service.GetHelloMessage(req.Name),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "Cannot generate response", http.StatusInternalServerError)
	}
}
