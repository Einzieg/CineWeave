package providergatewayserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/jackc/pgx/v5"
)

type Options struct {
	Providers       *provider.Service
	ServiceToken    string
	ReadinessChecks map[string]httpx.ReadinessCheck
}

func NewHandler(options Options) (http.Handler, error) {
	if options.Providers == nil {
		return nil, errors.New("Provider Gateway service is required")
	}
	handler := gatewayHandler{
		providers:    options.Providers,
		serviceToken: strings.TrimSpace(options.ServiceToken),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", httpx.HealthHandler("provider-gateway"))
	mux.HandleFunc(
		"/readyz",
		httpx.ReadyHandler("provider-gateway", options.ReadinessChecks),
	)
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
	mux.HandleFunc("/internal/provider/v1/managed-accounts/ensure", handler.withServiceAuth(handler.ensureManagedProviderAccount))
	mux.HandleFunc("/internal/provider/v1/credential-imports", handler.withServiceAuth(handler.importManagedCredential))
	mux.HandleFunc("/internal/provider/v1/credential-imports/resolve", handler.withServiceAuth(handler.resolveManagedCredential))
	mux.HandleFunc("/internal/provider/v1/credential-imports/activate", handler.withServiceAuth(handler.activateManagedCredential))
	mux.HandleFunc("/internal/provider/v1/credential-imports/revoke", handler.withServiceAuth(handler.revokeManagedCredential))
	return httpx.WithRequestID(mux), nil
}

func RunProviderRequestReconciler(ctx context.Context, providers *provider.Service, logger *slog.Logger, interval, staleAfter time.Duration) {
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

func (h gatewayHandler) ensureManagedProviderAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.EnsureManagedProviderAccountRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	response, err := h.providers.EnsureManagedProviderAccount(r.Context(), req)
	if err != nil {
		writeGatewayError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func (h gatewayHandler) importManagedCredential(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.ImportManagedCredentialRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	response, err := h.providers.ImportManagedCredential(r.Context(), req)
	if err != nil {
		writeGatewayError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, response, nil)
}

func (h gatewayHandler) resolveManagedCredential(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.ResolveManagedCredentialRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	response, err := h.providers.ResolveManagedCredential(r.Context(), req)
	if err != nil {
		writeGatewayError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func (h gatewayHandler) activateManagedCredential(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.ActivateManagedCredentialRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	response, err := h.providers.ActivateManagedCredential(r.Context(), req)
	if err != nil {
		writeGatewayError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, response, nil)
}

func (h gatewayHandler) revokeManagedCredential(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	var req provider.RevokeManagedCredentialRequest
	if !decodeGateway(w, r, &req) {
		return
	}
	if err := h.providers.RevokeManagedCredential(r.Context(), req); err != nil {
		writeGatewayError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"revoked": true}, nil)
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
	if errors.Is(err, provider.ErrConflict) {
		status = http.StatusConflict
		standard.Code = provider.CodeProviderIdempotencyConflict
		standard.Message = err.Error()
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
	if errors.Is(err, provider.ErrConflict) {
		return provider.StandardError{Code: provider.CodeProviderIdempotencyConflict, Message: err.Error(), Retryable: false}
	}
	return provider.StandardError{Code: provider.CodeUnknownError, Message: err.Error(), Retryable: false}
}
