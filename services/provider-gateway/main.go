package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/observability"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/service"
	"github.com/jackc/pgx/v5"
)

func main() {
	cfg := config.ServerFromEnv("provider-gateway", "CINEWEAVE_PROVIDER_GATEWAY_ADDR", ":8082")
	logger := observability.Logger(cfg.Name, cfg.Env)
	ctx := context.Background()

	pool, err := db.Open(ctx, config.Get("DATABASE_URL", "postgres://cineweave:cineweave_dev_password@localhost:5432/cineweave?sslmode=disable"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	credentialVault, err := provider.NewVaultFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	providerService := provider.NewService(pool, credentialVault)
	serviceToken := config.Get("CINEWEAVE_SERVICE_TOKEN", "dev-service-token")
	handler := gatewayHandler{providers: providerService, serviceToken: serviceToken}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", httpx.HealthHandler("provider-gateway"))
	mux.HandleFunc("/readyz", httpx.HealthHandler("provider-gateway"))
	mux.HandleFunc("/internal/provider/models/discover", handler.withServiceAuth(handler.discoverModels))
	mux.HandleFunc("/internal/provider/text/generate", handler.withServiceAuth(handler.generateText))
	mux.HandleFunc("/internal/provider/text/stream", handler.withServiceAuth(handler.streamText))
	mux.HandleFunc("/internal/provider/manifests/test-run", handler.withServiceAuth(handler.runManifestTest))
	mux.HandleFunc("/internal/provider/image/generate", httpx.NotImplemented("provider image generation"))
	mux.HandleFunc("/internal/provider/video/create-task", httpx.NotImplemented("provider video task creation"))
	mux.HandleFunc("/internal/provider/audio/tts", httpx.NotImplemented("provider audio tts"))

	if err := service.Serve(ctx, cfg, httpx.WithRequestID(mux), logger); err != nil {
		log.Fatal(err)
	}
}

type gatewayHandler struct {
	providers    *provider.Service
	serviceToken string
}

func (h gatewayHandler) withServiceAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(h.serviceToken) != "" && r.Header.Get("Authorization") != "Bearer "+h.serviceToken {
			httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "service token is invalid", nil, false)
			return
		}
		next(w, r)
	}
}

func (h gatewayHandler) discoverModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.GatewayDiscoverModelsRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	response, err := h.providers.DiscoverModelsViaGateway(r.Context(), req)
	if err != nil {
		writeGatewayError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func (h gatewayHandler) generateText(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.GatewayTextRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	response, err := h.providers.GenerateText(r.Context(), req)
	if err != nil {
		writeGatewayError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func (h gatewayHandler) streamText(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.GatewayTextRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	response, err := h.providers.StreamText(r.Context(), req, func(delta provider.GatewayTextDelta) error {
		if err := writeSSE(w, "provider.delta", delta); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		_ = writeSSE(w, "provider.error", standardGatewayError(err))
		if flusher != nil {
			flusher.Flush()
		}
		return
	}
	_ = writeSSE(w, "provider.completed", response)
	if flusher != nil {
		flusher.Flush()
	}
}

func (h gatewayHandler) runManifestTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.GatewayManifestTestRunRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	response, err := h.providers.RunManifestTest(r.Context(), req.OrganizationID, req.UserID, req.Request)
	if err != nil {
		writeGatewayError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func decodeGateway(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "request body is invalid", err.Error(), false)
		return false
	}
	return true
}

func writeSSE(w http.ResponseWriter, event string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte("event: " + event + "\n")); err != nil {
		return err
	}
	if _, err := w.Write([]byte("data: " + string(payload) + "\n\n")); err != nil {
		return err
	}
	return nil
}

func writeGatewayError(w http.ResponseWriter, r *http.Request, err error) {
	standard := standardGatewayError(err)
	status := http.StatusInternalServerError
	if errors.Is(err, provider.ErrValidation) {
		status = http.StatusUnprocessableEntity
	}
	if errors.Is(err, pgx.ErrNoRows) {
		status = http.StatusNotFound
		standard.Code = "NOT_FOUND"
		standard.Message = "resource was not found"
	}
	var upstreamErr *provider.UpstreamError
	if errors.As(err, &upstreamErr) {
		status = http.StatusBadGateway
	}
	httpx.WriteError(w, r, status, standard.Code, standard.Message, standard, standard.Retryable)
}

func standardGatewayError(err error) provider.StandardError {
	var upstreamErr *provider.UpstreamError
	if errors.As(err, &upstreamErr) {
		return provider.NormalizeHTTPError(upstreamErr.Status, upstreamErr.Code)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return provider.StandardError{Code: provider.CodeUpstreamTimeout, Message: "provider request timed out", Retryable: true}
	}
	if errors.Is(err, provider.ErrValidation) {
		return provider.StandardError{Code: provider.CodeInvalidRequest, Message: err.Error(), Retryable: false}
	}
	return provider.StandardError{Code: provider.CodeUnknownError, Message: err.Error(), Retryable: false}
}
