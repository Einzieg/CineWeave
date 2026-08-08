package projectcontrol

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu      sync.RWMutex
	actions map[string]Descriptor
}

func NewRegistry(descriptors ...Descriptor) (*Registry, error) {
	registry := &Registry{actions: make(map[string]Descriptor, len(descriptors))}
	for _, descriptor := range descriptors {
		if err := registry.Register(descriptor); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Register(descriptor Descriptor) error {
	if r == nil {
		return fmt.Errorf("project control registry is nil")
	}
	if err := descriptor.Validate(); err != nil {
		return err
	}
	name := strings.TrimSpace(descriptor.Name)
	descriptor.Name = name
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.actions[name]; exists {
		return fmt.Errorf("project control action %s is already registered", name)
	}
	r.actions[name] = descriptor.Clone()
	return nil
}

func (r *Registry) Get(name string) (Descriptor, bool) {
	if r == nil {
		return Descriptor{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	descriptor, ok := r.actions[strings.TrimSpace(name)]
	return descriptor.Clone(), ok
}

func (r *Registry) List() []Descriptor {
	if r == nil {
		return []Descriptor{}
	}
	r.mu.RLock()
	items := make([]Descriptor, 0, len(r.actions))
	for _, descriptor := range r.actions {
		items = append(items, descriptor.Clone())
	}
	r.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].Version < items[j].Version
		}
		return items[i].Name < items[j].Name
	})
	return items
}
