package provider

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestSSEDecoderSupportsLargeEvents(t *testing.T) {
	payload := strings.Repeat("x", 2<<20)
	decoder := newSSEDecoder(strings.NewReader("event: provider.delta\ndata: "+payload+"\n\n"), 3<<20)

	event, ok, err := decoder.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want event")
	}
	if event.Event != "provider.delta" {
		t.Fatalf("event = %q, want provider.delta", event.Event)
	}
	if event.Data != payload {
		t.Fatalf("data length = %d, want %d", len(event.Data), len(payload))
	}
}

func TestSSEDecoderReturnsUnexpectedEOF(t *testing.T) {
	decoder := newSSEDecoder(&unexpectedEOFReader{
		payload: "event: provider.completed\ndata: {\"status\":\"succeeded\"}",
	}, defaultSSEMaxEventBytes)

	_, ok, err := decoder.Next()
	if err == nil {
		t.Fatal("Next() error = nil, want unexpected EOF")
	}
	if ok {
		t.Fatal("Next() ok = true, want no dispatched event on unexpected EOF")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Next() error = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestSSEDecoderReturnsUnexpectedEOFStringAsError(t *testing.T) {
	decoder := newSSEDecoder(&unexpectedEOFStringReader{
		payload: "data: {\"status\":\"succeeded\"}",
	}, defaultSSEMaxEventBytes)

	_, ok, err := decoder.Next()
	if err == nil {
		t.Fatal("Next() error = nil, want unexpected EOF")
	}
	if ok {
		t.Fatal("Next() ok = true, want no dispatched event on unexpected EOF string")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Next() error = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestSSEDecoderRejectsOversizedLine(t *testing.T) {
	decoder := newSSEDecoder(strings.NewReader("data: 123456\n\n"), 4)

	_, _, err := decoder.Next()
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Next() error = %v, want ErrValidation", err)
	}
}

func TestSSEDecoderJoinsMultilineData(t *testing.T) {
	decoder := newSSEDecoder(strings.NewReader("event: provider.delta\ndata: first\ndata: second\n\n"), defaultSSEMaxEventBytes)

	event, ok, err := decoder.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want event")
	}
	if event.Data != "first\nsecond" {
		t.Fatalf("data = %q, want joined multiline data", event.Data)
	}
}

type unexpectedEOFReader struct {
	payload string
	sent    bool
}

func (r *unexpectedEOFReader) Read(p []byte) (int, error) {
	if r.sent {
		return 0, io.ErrUnexpectedEOF
	}
	r.sent = true
	return copy(p, r.payload), io.ErrUnexpectedEOF
}

type unexpectedEOFStringReader struct {
	payload string
	sent    bool
}

func (r *unexpectedEOFStringReader) Read(p []byte) (int, error) {
	if r.sent {
		return 0, errors.New("upstream unexpected EOF")
	}
	r.sent = true
	return copy(p, r.payload), errors.New("upstream unexpected EOF")
}
