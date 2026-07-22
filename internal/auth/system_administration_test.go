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
