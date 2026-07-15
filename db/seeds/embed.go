package seeds

import "embed"

// FS contains versioned system-owned business resources. These files are
// intentionally separate from schema migrations.
//
//go:embed rbac/*.json provider-catalog/*.json model-capabilities/*.json prompt-registry/*.json project-manuals/*.json
var FS embed.FS
