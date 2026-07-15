package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/observability"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/service"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/jackc/pgx/v5"
)

func main() {
	cfg := config.ServerFromEnv("provider-gateway", "CINEWEAVE_PROVIDER_GATEWAY_ADDR", ":8082")
	logger := observability.Logger(cfg.Name, cfg.Env)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
	providerService.EnableGatewayRuntime()
	storageClient, err := storage.New(ctx, storage.ConfigFromEnv())
	if err != nil {
		log.Fatal(err)
	}
	providerService.SetStorage(storageClient)
	go runProviderRequestReconciler(
		ctx,
		providerService,
		logger,
		providerReconcileDuration("CINEWEAVE_PROVIDER_REQUEST_RECONCILE_INTERVAL", time.Minute),
		providerReconcileDuration("CINEWEAVE_PROVIDER_REQUEST_STALE_AFTER", 30*time.Minute),
	)
	serviceToken := config.Get("CINEWEAVE_SERVICE_TOKEN", config.DefaultServiceToken)
	if err := config.ValidateProductionSecret(cfg.Env, "CINEWEAVE_SERVICE_TOKEN", serviceToken, config.DefaultServiceToken); err != nil {
		log.Fatal(err)
	}
	handler := gatewayHandler{providers: providerService, serviceToken: serviceToken}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", httpx.HealthHandler("provider-gateway"))
	mux.HandleFunc("/readyz", httpx.ReadyHandler("provider-gateway", map[string]httpx.ReadinessCheck{
		"database": func(checkCtx context.Context) error {
			pingCtx, cancel := context.WithTimeout(checkCtx, 2*time.Second)
			defer cancel()
			return pool.Ping(pingCtx)
		},
		"storage": func(checkCtx context.Context) error {
			pingCtx, cancel := context.WithTimeout(checkCtx, 2*time.Second)
			defer cancel()
			return storageClient.Ping(pingCtx)
		},
	}))
	mux.HandleFunc("/internal/provider/models/discover", handler.withServiceAuth(handler.discoverModels))
	mux.HandleFunc("/internal/provider/models/constraints", handler.withServiceAuth(handler.resolveModelConstraints))
	mux.HandleFunc("/internal/provider/text/generate", handler.withServiceAuth(handler.generateText))
	mux.HandleFunc("/internal/provider/text/stream", handler.withServiceAuth(handler.streamText))
	mux.HandleFunc("/internal/provider/manifests/test-run", handler.withServiceAuth(handler.runManifestTest))
	mux.HandleFunc("/internal/provider/image/generate", handler.withServiceAuth(handler.generateImage))
	mux.HandleFunc("/internal/provider/video/plan", handler.withServiceAuth(handler.planVideo))
	mux.HandleFunc("/internal/provider/video/retry-segment", handler.withServiceAuth(handler.retryVideoSegment))
	mux.HandleFunc("/internal/provider/video/create-task", handler.withServiceAuth(handler.createVideoTask))
	mux.HandleFunc("/internal/provider/video/poll-task", handler.withServiceAuth(handler.pollVideoTask))
	mux.HandleFunc("/internal/provider/video/cancel-task", handler.withServiceAuth(handler.cancelVideoTask))
	mux.HandleFunc("/internal/provider/audio/tts", handler.withServiceAuth(handler.generateSpeech))
	mux.HandleFunc("/internal/provider/audio/transcribe", handler.withServiceAuth(handler.transcribeAudio))

	if err := service.Serve(ctx, cfg, httpx.WithRequestID(mux), logger); err != nil {
		log.Fatal(err)
	}
}

func runProviderRequestReconciler(ctx context.Context, providers *provider.Service, logger *slog.Logger, interval, staleAfter time.Duration) {
	reconcile := func() {
		count, err := providers.ReconcileStaleProviderRequests(ctx, staleAfter, 100)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				logger.Error("provider request reconciliation failed", "error", err)
			}
			return
		}
		if count > 0 {
			logger.Warn("provider requests marked unknown", "count", count, "staleAfter", staleAfter.String())
		}
	}
	reconcile()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		}
	}
}

func providerReconcileDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(config.Get(key, ""))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
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

func (h gatewayHandler) resolveModelConstraints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.GatewayModelConstraintsRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	response, err := h.providers.ResolveModelConstraints(r.Context(), req)
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

func (h gatewayHandler) generateImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.GatewayImageRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	response, err := h.providers.GenerateImage(r.Context(), req)
	if err != nil {
		writeGatewayError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func (h gatewayHandler) generateSpeech(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.GatewayTTSRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	response, err := h.providers.GenerateSpeech(r.Context(), req)
	if err != nil {
		writeGatewayError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func (h gatewayHandler) transcribeAudio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.GatewayASRRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	response, err := h.providers.TranscribeAudio(r.Context(), req)
	if err != nil {
		writeGatewayError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func (h gatewayHandler) createVideoTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.GatewayVideoCreateTaskRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	response, err := h.providers.CreateVideoTask(r.Context(), req)
	if err != nil {
		writeGatewayError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func (h gatewayHandler) planVideo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.GatewayVideoPlanRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	response, err := h.providers.PlanVideo(r.Context(), req)
	if err != nil {
		writeGatewayError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func (h gatewayHandler) retryVideoSegment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.GatewayVideoRetrySegmentRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	response, err := h.providers.RetryVideoRenderSegment(r.Context(), req)
	if err != nil {
		writeGatewayError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func (h gatewayHandler) pollVideoTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.GatewayVideoPollTaskRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	response, err := h.providers.PollVideoTask(r.Context(), req)
	if err != nil {
		writeGatewayError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func (h gatewayHandler) cancelVideoTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.GatewayVideoCancelTaskRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	response, err := h.providers.CancelVideoTask(r.Context(), req)
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
	_, err := h.providers.StreamTextEvents(r.Context(), req, func(event provider.GatewayTextStreamEvent) error {
		if err := writeSSE(w, event.Type, event.Payload()); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		if flusher != nil {
			flusher.Flush()
		}
		return
	}
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
	if standardErr, ok := provider.StandardErrorFromError(err); ok {
		standard = *standardErr
		status = provider.HTTPStatusForStandardError(standardErr)
	}
	if _, ok := provider.StandardErrorFromGuard(err); ok {
		status = http.StatusTooManyRequests
		if standard.Code == provider.CodeProviderDailyQuotaExceeded || standard.Code == provider.CodeProviderMonthlyBudgetExceeded {
			status = http.StatusPaymentRequired
		}
	}
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
		status = provider.HTTPStatusForStandardError(&standard)
	}
	httpx.WriteError(w, r, status, standard.Code, standard.Message, standard, standard.Retryable)
}

func standardGatewayError(err error) provider.StandardError {
	if standard, ok := provider.StandardErrorFromError(err); ok {
		return *standard
	}
	if standard, ok := provider.StandardErrorFromGuard(err); ok {
		return *standard
	}
	var upstreamErr *provider.UpstreamError
	if errors.As(err, &upstreamErr) {
		return provider.NormalizeUpstreamError(upstreamErr)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return provider.StandardError{Code: provider.CodeUpstreamTimeout, Message: "provider request timed out", Retryable: true}
	}
	if errors.Is(err, provider.ErrValidation) {
		return provider.StandardError{Code: provider.CodeInvalidRequest, Message: err.Error(), Retryable: false}
	}
	return provider.StandardError{Code: provider.CodeUnknownError, Message: err.Error(), Retryable: false}
}
