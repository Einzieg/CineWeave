package main

import (
	"context"
	"log"
	"net/http"

	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/observability"
	"github.com/Einzieg/cineweave/internal/service"
)

func main() {
	cfg := config.ServerFromEnv("provider-gateway", "CINEWEAVE_PROVIDER_GATEWAY_ADDR", ":8082")
	logger := observability.Logger(cfg.Name, cfg.Env)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", httpx.HealthHandler("provider-gateway"))
	mux.HandleFunc("/readyz", httpx.HealthHandler("provider-gateway"))
	mux.HandleFunc("/internal/provider/text/generate", httpx.NotImplemented("provider text generation"))
	mux.HandleFunc("/internal/provider/text/stream", httpx.NotImplemented("provider text streaming"))
	mux.HandleFunc("/internal/provider/image/generate", httpx.NotImplemented("provider image generation"))
	mux.HandleFunc("/internal/provider/video/create-task", httpx.NotImplemented("provider video task creation"))
	mux.HandleFunc("/internal/provider/audio/tts", httpx.NotImplemented("provider audio tts"))

	if err := service.Serve(context.Background(), cfg, httpx.WithRequestID(mux), logger); err != nil {
		log.Fatal(err)
	}
}
