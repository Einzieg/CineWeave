package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestCreateSystemOrganizationRejectsInvalidInputBeforeDatabaseAccess(t *testing.T) {
	tests := []CreateSystemOrganizationRequest{
		{Name: "", OwnerIdentifier: "owner"},
		{Name: strings.Repeat("组", 101), OwnerIdentifier: "owner"},
		{Name: "有效组织", WorkspaceName: strings.Repeat("工", 101), OwnerIdentifier: "owner"},
		{Name: "有效组织", OwnerIdentifier: ""},
	}
	for _, req := range tests {
		if _, _, _, _, err := normalizeCreateSystemOrganizationRequest(req); !errors.Is(err, ErrSystemOrganizationValidation) {
			t.Fatalf("normalizeCreateSystemOrganizationRequest(%+v) error = %v", req, err)
		}
	}
}

func TestNormalizeSystemPagination(t *testing.T) {
	page, pageSize := normalizeSystemPagination(0, 0)
	if page != 1 || pageSize != 25 {
		t.Fatalf("default pagination = %d/%d", page, pageSize)
	}
	page, pageSize = normalizeSystemPagination(3, 500)
	if page != 3 || pageSize != 100 {
		t.Fatalf("clamped pagination = %d/%d", page, pageSize)
	}
}

func TestNormalizeCreateSystemOrganizationMemberRequest(t *testing.T) {
	newAccount, err := normalizeCreateSystemOrganizationMemberRequest(CreateSystemOrganizationMemberRequest{
		Email:       "  MEMBER@Example.Test ",
		Username:    "Member_User",
		Password:    "Password123!",
		DisplayName: "  新成员  ",
	})
	if err != nil {
		t.Fatalf("normalize new account: %v", err)
	}
	if newAccount.AttachExisting || newAccount.Email != "member@example.test" || newAccount.Username != "Member_User" ||
		newAccount.UsernameNormalized != "member_user" || newAccount.DisplayName != "新成员" {
		t.Fatalf("normalized new account = %+v", newAccount)
	}

	existing, err := normalizeCreateSystemOrganizationMemberRequest(CreateSystemOrganizationMemberRequest{
		ExistingUserIdentifier: "  Existing_User ",
	})
	if err != nil {
		t.Fatalf("normalize existing account: %v", err)
	}
	if !existing.AttachExisting || existing.ExistingIdentifier != "existing_user" || existing.ExistingIdentifierIsEmail {
		t.Fatalf("normalized existing account = %+v", existing)
	}

	invalid := []CreateSystemOrganizationMemberRequest{
		{},
		{ExistingUserIdentifier: "member", Email: "member@example.test"},
		{Email: "invalid", Username: "member-user", Password: "Password123!"},
		{Email: "member@example.test", Username: "root", Password: "Password123!"},
		{Email: "member@example.test", Username: "member-user", Password: "short"},
	}
	for _, req := range invalid {
		if _, err := normalizeCreateSystemOrganizationMemberRequest(req); !errors.Is(err, ErrSystemMemberValidation) {
			t.Fatalf("normalizeCreateSystemOrganizationMemberRequest(%+v) error = %v", req, err)
		}
	}
}

func TestNormalizeUpdateSystemOrganizationMemberRequest(t *testing.T) {
	email := " NEW@Example.Test "
	username := "New_User"
	displayName := "  新名称 "
	status := "disabled"
	password := "NewPassword456!"
	normalized, err := normalizeUpdateSystemOrganizationMemberRequest(UpdateSystemOrganizationMemberRequest{
		Email:       &email,
		Username:    &username,
		DisplayName: &displayName,
		Password:    &password,
		Status:      &status,
	})
	if err != nil {
		t.Fatalf("normalize update: %v", err)
	}
	if normalized.Email != "new@example.test" || normalized.Username != "New_User" ||
		normalized.UsernameNormalized != "new_user" || normalized.DisplayName != "新名称" ||
		normalized.Status != "disabled" || normalized.Password != password {
		t.Fatalf("normalized update = %+v", normalized)
	}

	invalidStatus := "removed"
	invalid := []UpdateSystemOrganizationMemberRequest{
		{},
		{Status: &invalidStatus},
	}
	for _, req := range invalid {
		if _, err := normalizeUpdateSystemOrganizationMemberRequest(req); !errors.Is(err, ErrSystemMemberValidation) {
			t.Fatalf("normalizeUpdateSystemOrganizationMemberRequest(%+v) error = %v", req, err)
		}
	}
}
