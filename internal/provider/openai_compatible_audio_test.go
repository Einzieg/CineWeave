package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompatibleAudioSpeech(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["model"] != "gpt-4o-mini-tts" || request["input"] != "必须保留的中文台词" || request["voice"] != "alloy" || request["response_format"] != "wav" {
			t.Fatalf("request = %#v", request)
		}
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte("RIFF\x04\x00\x00\x00WAVE"))
	}))
	defer server.Close()

	account := Account{BaseURL: &server.URL, AuthType: "bearer"}
	model := Model{ModelKey: "gpt-4o-mini-tts"}
	result, err := newOpenAICompatibleClient(2*time.Second).audioSpeech(
		context.Background(), account, model, "sk-test", parseOpenAICompatibleConfig(nil),
		json.RawMessage(`{"input":"必须保留的中文台词","voice":"alloy","response_format":"wav"}`),
	)
	if err != nil {
		t.Fatalf("audioSpeech: %v", err)
	}
	defer result.close()
	body, readErr := os.ReadFile(result.TempPath)
	if readErr != nil {
		t.Fatalf("read spooled audio: %v", readErr)
	}
	if result.MimeType != "audio/wav" || string(body) != "RIFF\x04\x00\x00\x00WAVE" {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenAICompatibleAudioTranscription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		defer file.Close()
		body, _ := io.ReadAll(file)
		if string(body) != "audio-body" || r.FormValue("model") != "whisper-1" || r.FormValue("language") != "zh" {
			t.Fatalf("multipart body/model/language = %q/%q/%q", body, r.FormValue("model"), r.FormValue("language"))
		}
		if strings.Join(r.MultipartForm.Value["timestamp_granularities[]"], ",") != "segment,word" {
			t.Fatalf("timestamp granularities = %#v", r.MultipartForm.Value)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"必须保留的中文台词","language":"zh","duration":2.5,"segments":[{"id":0,"text":"必须保留的中文台词","start":0,"end":2.5}]}`))
	}))
	defer server.Close()

	account := Account{BaseURL: &server.URL, AuthType: "bearer"}
	model := Model{ModelKey: "whisper-1"}
	result, err := newOpenAICompatibleClient(2*time.Second).audioTranscription(
		context.Background(), account, model, "sk-test", parseOpenAICompatibleConfig(nil),
		json.RawMessage(`{"language":"zh"}`), []byte("audio-body"), "audio/wav", "line.wav",
	)
	if err != nil {
		t.Fatalf("audioTranscription: %v", err)
	}
	if result.Output.Text != "必须保留的中文台词" || result.Output.Language != "zh" || len(result.Output.Segments) != 1 {
		t.Fatalf("output = %+v", result.Output)
	}
}

func TestAudioSpeechRequestRejectsMissingVoice(t *testing.T) {
	_, _, err := buildAudioSpeechRequest("tts-model", json.RawMessage(`{"input":"台词"}`))
	if err == nil {
		t.Fatal("expected missing voice error")
	}
}

func TestBuildProviderURLNormalizesOpenAICompatibleAudioV1(t *testing.T) {
	base := "https://newapi.example.com"
	for _, endpoint := range []string{"/audio/speech", "/audio/transcriptions"} {
		got, err := buildProviderURL(&base, endpoint, true)
		if err != nil {
			t.Fatalf("buildProviderURL: %v", err)
		}
		if got != "https://newapi.example.com/v1"+endpoint {
			t.Fatalf("url = %q", got)
		}
	}
}
