package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const mockVideoMP4Base64 = "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"

type mockProvider struct {
	mu            sync.Mutex
	counter       atomic.Uint64
	tasks         map[string]*videoTask
	publicBaseURL string
	videoBytes    []byte
}

type videoTask struct {
	ID        string
	PollCount int
	FileName  string
	Cancelled bool
}

func main() {
	addr := env("CINEWEAVE_MOCK_PROVIDER_ADDR", ":19180")
	publicBaseURL := strings.TrimRight(env("CINEWEAVE_MOCK_PROVIDER_PUBLIC_BASE_URL", "http://mock-provider:19180"), "/")
	videoBytes, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(mockVideoMP4Base64, " ", ""))
	if err != nil {
		log.Fatalf("decode mock video: %v", err)
	}

	provider := &mockProvider{
		tasks:         map[string]*videoTask{},
		publicBaseURL: publicBaseURL,
		videoBytes:    videoBytes,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", provider.healthz)
	mux.HandleFunc("GET /v1/models", provider.models)
	mux.HandleFunc("POST /v1/chat/completions", provider.chatCompletions)
	mux.HandleFunc("POST /v1/images/generations", provider.imageGenerations)
	mux.HandleFunc("POST /video/create", provider.videoCreate)
	mux.HandleFunc("POST /video/poll", provider.videoPollPost)
	mux.HandleFunc("GET /video/poll/{taskId}", provider.videoPollGet)
	mux.HandleFunc("POST /video/cancel", provider.videoCancel)
	mux.HandleFunc("GET /files/video-1.mp4", provider.videoFile)
	mux.HandleFunc("GET /files/video-2.mp4", provider.videoFile)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("mock provider listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func (m *mockProvider) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (m *mockProvider) models(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data": []map[string]any{
			{"id": "cw-mock-text", "object": "model", "created": 0, "owned_by": "cineweave"},
			{"id": "cw-mock-image", "object": "model", "created": 0, "owned_by": "cineweave"},
		},
	})
}

func (m *mockProvider) chatCompletions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	content := storyboardJSON()
	if req.Stream {
		m.streamChatCompletion(w, firstNonEmpty(req.Model, "cw-mock-text"), content)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      "chatcmpl-cw-mock",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   firstNonEmpty(req.Model, "cw-mock-text"),
		"choices": []map[string]any{{
			"index":         0,
			"finish_reason": "stop",
			"message": map[string]string{
				"role":    "assistant",
				"content": content,
			},
		}},
		"usage": map[string]int{
			"prompt_tokens":     12,
			"completion_tokens": 120,
			"total_tokens":      132,
		},
	})
}

func (m *mockProvider) streamChatCompletion(w http.ResponseWriter, model, content string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	for _, chunk := range chunks(content, 120) {
		payload := map[string]any{
			"id":      "chatcmpl-cw-mock-stream",
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]string{"content": chunk},
			}},
		}
		writeSSE(w, payload)
		if flusher != nil {
			flusher.Flush()
		}
	}
	writeSSE(w, map[string]any{
		"id":      "chatcmpl-cw-mock-stream",
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{
			"index":         0,
			"delta":         map[string]string{},
			"finish_reason": "stop",
		}},
		"usage": map[string]int{"prompt_tokens": 12, "completion_tokens": 120, "total_tokens": 132},
	})
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func (m *mockProvider) imageGenerations(w http.ResponseWriter, r *http.Request) {
	imageBytes, err := mockPNG()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]string{"message": err.Error()}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"created": time.Now().Unix(),
		"data": []map[string]any{{
			"b64_json":       base64.StdEncoding.EncodeToString(imageBytes),
			"mime_type":      "image/png",
			"revised_prompt": "CineWeave mock storyboard frame",
		}},
	})
}

func (m *mockProvider) videoCreate(w http.ResponseWriter, r *http.Request) {
	n := m.counter.Add(1)
	taskID := "cw-mock-video-" + strconv.FormatUint(n, 10)
	fileName := "video-1.mp4"
	if n%2 == 0 {
		fileName = "video-2.mp4"
	}
	m.mu.Lock()
	m.tasks[taskID] = &videoTask{ID: taskID, FileName: fileName}
	m.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"taskId": taskID,
		"status": "running",
	})
}

func (m *mockProvider) videoPollPost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TaskID string `json:"taskId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	m.writePoll(w, req.TaskID)
}

func (m *mockProvider) videoPollGet(w http.ResponseWriter, r *http.Request) {
	m.writePoll(w, r.PathValue("taskId"))
}

func (m *mockProvider) writePoll(w http.ResponseWriter, taskID string) {
	task, ok := m.nextPoll(taskID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]string{"code": "TASK_NOT_FOUND", "message": "task not found"}})
		return
	}
	if task.Cancelled {
		writeJSON(w, http.StatusOK, map[string]any{"taskId": task.ID, "status": "cancelled"})
		return
	}
	if task.PollCount <= 1 {
		writeJSON(w, http.StatusOK, map[string]any{"taskId": task.ID, "status": "running"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"taskId":          task.ID,
		"status":          "succeeded",
		"videoUrl":        m.publicBaseURL + "/files/" + task.FileName,
		"mimeType":        "video/mp4",
		"durationSeconds": 0.5,
	})
}

func (m *mockProvider) nextPoll(taskID string) (videoTask, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[strings.TrimSpace(taskID)]
	if !ok {
		return videoTask{}, false
	}
	task.PollCount++
	return *task, true
}

func (m *mockProvider) videoCancel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TaskID string `json:"taskId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	m.mu.Lock()
	task, ok := m.tasks[strings.TrimSpace(req.TaskID)]
	if ok {
		task.Cancelled = true
	}
	m.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]string{"code": "TASK_NOT_FOUND", "message": "task not found"}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"taskId": task.ID, "status": "cancelled"})
}

func (m *mockProvider) videoFile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Length", strconv.Itoa(len(m.videoBytes)))
	_, _ = w.Write(m.videoBytes)
}

func storyboardJSON() string {
	return `{"title":"CineWeave Mock Silent Video","summary":"A deterministic two-shot silent video storyboard for smoke tests.","shots":[{"shotNo":1,"title":"Arrival","duration":2,"visual":"A quiet train platform at sunrise with light mist.","camera":"wide establishing shot","motion":"slow push-in","mood":"calm","imagePrompt":"Cinematic sunrise train platform, soft mist, wide shot","videoPrompt":"A silent sunrise train platform shot with a slow push-in and drifting mist."},{"shotNo":2,"title":"Departure","duration":2,"visual":"Warm sunlight crossing empty rails as the scene resolves.","camera":"low angle tracking shot","motion":"gentle lateral move","mood":"hopeful","imagePrompt":"Warm sunlight over empty rails, cinematic detail","videoPrompt":"A silent cinematic rail shot with warm sunlight and gentle lateral movement."}]}`
}

func mockPNG() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 256, 144))
	for y := 0; y < 144; y++ {
		for x := 0; x < 256; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(32 + x/3),
				G: uint8(80 + y/3),
				B: uint8(150 + (x+y)/8),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	return buf.Bytes(), err
}

func chunks(value string, size int) []string {
	if size <= 0 || len(value) <= size {
		return []string{value}
	}
	out := make([]string, 0, len(value)/size+1)
	for len(value) > size {
		out = append(out, value[:size])
		value = value[size:]
	}
	if value != "" {
		out = append(out, value)
	}
	return out
}

func writeSSE(w http.ResponseWriter, value any) {
	raw, _ := json.Marshal(value)
	fmt.Fprintf(w, "data: %s\n\n", raw)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func env(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
