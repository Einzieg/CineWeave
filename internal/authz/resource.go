package authz

type ResourceType string

const (
	ResourceOrganization ResourceType = "organization"
	ResourceWorkspace    ResourceType = "workspace"
	ResourceProject      ResourceType = "project"
)

type Resource struct {
	Type           ResourceType
	OrganizationID string
	WorkspaceID    string
	ProjectID      string
}
