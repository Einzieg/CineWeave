package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/httpx"
)

func TestMemberAccountProtectedErrorMapping(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/organizations/org-id/members/user-id/password-reset", nil)

	var server Server
	server.writeError(recorder, request, auth.ErrMemberAccountProtected)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	var envelope httpx.Envelope
	if err := json.NewDecoder(recorder.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != "MEMBER_ACCOUNT_PROTECTED" {
		t.Fatalf("error = %+v, want MEMBER_ACCOUNT_PROTECTED", envelope.Error)
	}
	if envelope.Error.Message != "member account is protected" {
		t.Fatalf("message = %q, want generic protected-account message", envelope.Error.Message)
	}
}
