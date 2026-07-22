package api

import "testing"

func TestPermissionAllowedForRoleScope(t *testing.T) {
	tests := []struct {
		scope      string
		permission string
		want       bool
	}{
		{scope: "organization", permission: "member.manage", want: true},
		{scope: "organization", permission: "admin.manage", want: false},
		{scope: "workspace", permission: "workspace.manage", want: true},
		{scope: "workspace", permission: "project.create", want: true},
		{scope: "workspace", permission: "provider.read", want: false},
		{scope: "project", permission: "project.read", want: true},
		{scope: "project", permission: "project.video_production.rebuild", want: true},
		{scope: "project", permission: "workflow.run", want: true},
		{scope: "project", permission: "workspace.read", want: false},
		{scope: "unknown", permission: "project.read", want: false},
	}
	for _, test := range tests {
		t.Run(test.scope+"/"+test.permission, func(t *testing.T) {
			if got := permissionAllowedForRoleScope(test.scope, test.permission); got != test.want {
				t.Fatalf("permissionAllowedForRoleScope(%q, %q) = %v, want %v", test.scope, test.permission, got, test.want)
			}
		})
	}
}
