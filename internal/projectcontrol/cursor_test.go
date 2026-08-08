package projectcontrol

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCommandCursorRoundTrip(t *testing.T) {
	want := CommandCursor{CreatedAt: time.Date(2026, time.August, 7, 12, 30, 0, 123, time.UTC), ID: uuid.NewString()}
	encoded, err := EncodeCommandCursor(want)
	if err != nil {
		t.Fatalf("EncodeCommandCursor: %v", err)
	}
	got, err := DecodeCommandCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCommandCursor: %v", err)
	}
	if got == nil || got.ID != want.ID || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("cursor = %#v, want %#v", got, want)
	}
}

func TestDecodeCommandCursorRejectsInvalidValue(t *testing.T) {
	if _, err := DecodeCommandCursor("not-a-cursor"); err == nil {
		t.Fatal("DecodeCommandCursor succeeded, want error")
	}
}
