package agent

import (
	"fmt"
	"sort"
)

type Registry struct {
	tools map[string]AgentTool
}

func NewRegistry(tools ...AgentTool) (*Registry, error) {
	registry := &Registry{tools: map[string]AgentTool{}}
	for _, tool := range tools {
		if err := registry.Register(tool); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Register(tool AgentTool) error {
	if tool.Name == "" {
		return fmt.Errorf("agent tool name is required")
	}
	if tool.Risk == "" {
		return fmt.Errorf("agent tool %s risk is required", tool.Name)
	}
	if _, exists := r.tools[tool.Name]; exists {
		return fmt.Errorf("agent tool %s is already registered", tool.Name)
	}
	r.tools[tool.Name] = tool
	return nil
}

func (r *Registry) Get(name string) (AgentTool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

func (r *Registry) List() []AgentTool {
	items := make([]AgentTool, 0, len(r.tools))
	for _, tool := range r.tools {
		items = append(items, tool)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items
}

func (r *Registry) Descriptors() []ToolDescriptor {
	tools := r.List()
	items := make([]ToolDescriptor, 0, len(tools))
	for _, tool := range tools {
		items = append(items, tool.Descriptor())
	}
	return items
}
