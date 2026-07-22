package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAccessTokenCarriesCredentialAndMembershipVersions(t *testing.T) {
	service := &Service{jwtSecret: []byte("credential-version-test"), accessTTL: time.Hour}
	token, err := service.accessToken("4e21e6ba-8136-4561-94c7-559af1fef6fc", "b377b68f-58e8-49d4-a29c-2cbcf34da109", 7, 11)
	if err != nil {
		t.Fatalf("create access token: %v", err)
	}
	principal, err := service.ParseBearer("Bearer " + token)
	if err != nil {
		t.Fatalf("parse access token: %v", err)
	}
	if principal.CredentialVersion != 7 {
		t.Fatalf("credential version = %d, want 7", principal.CredentialVersion)
	}
	if principal.MembershipAuthorizationVersion != 11 {
		t.Fatalf("membership authorization version = %d, want 11", principal.MembershipAuthorizationVersion)
	}
}

func TestNormalizeMemberProfileUpdate(t *testing.T) {
	displayName := "  Member Name  "
	avatarURL := " https://example.test/avatar.png "
	gotName, gotAvatar, fields, err := normalizeMemberProfileUpdate(UpdateProfileRequest{DisplayName: &displayName, AvatarURL: &avatarURL})
	if err != nil {
		t.Fatalf("normalize profile: %v", err)
	}
	if gotName != "Member Name" || gotAvatar != "https://example.test/avatar.png" || strings.Join(fields, ",") != "displayName,avatarUrl" {
		t.Fatalf("normalized profile = %q, %q, %v", gotName, gotAvatar, fields)
	}

	invalidURL := "javascript:alert(1)"
	if _, _, _, err := normalizeMemberProfileUpdate(UpdateProfileRequest{AvatarURL: &invalidURL}); !errors.Is(err, ErrMemberProfileValidation) {
		t.Fatalf("invalid avatar error = %v, want ErrMemberProfileValidation", err)
	}

	tooLong := strings.Repeat("名", 101)
	if _, _, _, err := normalizeMemberProfileUpdate(UpdateProfileRequest{DisplayName: &tooLong}); !errors.Is(err, ErrMemberProfileValidation) {
		t.Fatalf("long display name error = %v, want ErrMemberProfileValidation", err)
	}
}
