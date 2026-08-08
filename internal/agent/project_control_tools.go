package agent

import "sort"

// ProjectControlTools returns the complete, de-duplicated tool surface that can
// be shared by the embedded assistant and external project-control adapters.
// Common tools retain every project kind instead of being registered twice.
func ProjectControlTools() []AgentTool {
	byName := make(map[string]AgentTool)
	for _, projectKind := range []string{ProjectKindNarrative, ProjectKindCommerceVideo} {
		policy, err := PolicyForProjectKind(projectKind)
		if err != nil {
			panic(err)
		}
		for _, tool := range policy.Tools() {
			if existing, ok := byName[tool.Name]; ok {
				existing.ProjectKinds = appendUniqueStrings(existing.ProjectKinds, tool.ProjectKinds...)
				byName[tool.Name] = existing
				continue
			}
			tool.ProjectKinds = appendUniqueStrings(nil, tool.ProjectKinds...)
			byName[tool.Name] = tool
		}
	}

	tools := make([]AgentTool, 0, len(byName))
	for _, tool := range byName {
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	result := make([]string, 0, len(values)+len(additions))
	for _, value := range append(append([]string(nil), values...), additions...) {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
