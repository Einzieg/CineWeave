package projectcontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	DefaultMaximumDispatchAttempts = 3
	defaultReconcileDelay          = 5 * time.Second
)

type DispatchRequest struct {
	Command       Command
	Items         []CommandItem
	AttemptNumber int
}

func (r DispatchRequest) TemporalWorkflowID(itemKey string) (string, error) {
	return TemporalWorkflowIdentity(r.Command.ID, itemKey, r.Command.ActionVersion)
}

func (r DispatchRequest) ItemByKey(itemKey string) (CommandItem, bool) {
	for _, item := range r.Items {
		if item.ItemKey == itemKey {
			return item, true
		}
	}
	return CommandItem{}, false
}

type DispatchPrompt struct {
	Kind               string
	Prompt             string
	Options            json.RawMessage
	CandidateRevisions json.RawMessage
	ExpiresAt          time.Time
}

type DispatchOutcome struct {
	Output             json.RawMessage
	WorkflowLinks      []WorkflowLink
	ItemResults        []ItemResult
	Prompt             *DispatchPrompt
	NextReconcileAfter time.Duration
}

type Handler interface {
	Execute(context.Context, DispatchRequest) (DispatchOutcome, error)
}

type RuntimeRepository interface {
	Get(context.Context, string) (Command, error)
	ClaimDispatch(context.Context, string, string, time.Duration) (*Claim, error)
	ClaimReconcile(context.Context, string, string, time.Duration) (*Claim, error)
	Items(context.Context, string) ([]CommandItem, error)
	WorkflowLinks(context.Context, string) ([]WorkflowLink, error)
	ApplyItemResults(context.Context, string, int64, []ItemResult) (Command, error)
	AttachWorkflows(context.Context, string, int64, []WorkflowLink, time.Time) (Command, error)
	CreatePrompt(context.Context, CreateCommandPrompt) (CommandPrompt, Command, error)
	ExpireNextPrompt(context.Context) (Command, bool, error)
	RescheduleReconcile(context.Context, string, int64, time.Time) (Command, error)
	Transition(context.Context, TransitionCommand) (Command, error)
	FinishAttempt(context.Context, string, string, string, string) error
}

type HandlerFunc func(context.Context, DispatchRequest) (DispatchOutcome, error)

func (f HandlerFunc) Execute(ctx context.Context, request DispatchRequest) (DispatchOutcome, error) {
	return f(ctx, request)
}

type RegisteredHandler struct {
	Descriptor Descriptor
	Handler    Handler
}

type RuntimeRegistry struct {
	mu       sync.RWMutex
	handlers map[string]RegisteredHandler
}

func NewRuntimeRegistry(registrations ...RegisteredHandler) (*RuntimeRegistry, error) {
	registry := &RuntimeRegistry{handlers: make(map[string]RegisteredHandler, len(registrations))}
	for _, registration := range registrations {
		if err := registry.Register(registration.Descriptor, registration.Handler); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *RuntimeRegistry) Register(descriptor Descriptor, handler Handler) error {
	if r == nil {
		return fmt.Errorf("project control runtime registry is nil")
	}
	if err := descriptor.Validate(); err != nil {
		return err
	}
	if handler == nil {
		return fmt.Errorf("project control action %s handler is required", descriptor.Name)
	}
	key := runtimeHandlerKey(descriptor.Name, descriptor.Version)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[key]; exists {
		return fmt.Errorf("project control action %s version %d is already registered", descriptor.Name, descriptor.Version)
	}
	descriptor.Name = strings.TrimSpace(descriptor.Name)
	r.handlers[key] = RegisteredHandler{Descriptor: descriptor.Clone(), Handler: handler}
	return nil
}

func (r *RuntimeRegistry) Get(actionName string, actionVersion int) (RegisteredHandler, bool) {
	if r == nil {
		return RegisteredHandler{}, false
	}
	r.mu.RLock()
	registration, ok := r.handlers[runtimeHandlerKey(actionName, actionVersion)]
	r.mu.RUnlock()
	registration.Descriptor = registration.Descriptor.Clone()
	return registration, ok
}

func (r *RuntimeRegistry) List() []Descriptor {
	if r == nil {
		return []Descriptor{}
	}
	r.mu.RLock()
	descriptors := make([]Descriptor, 0, len(r.handlers))
	for _, registration := range r.handlers {
		descriptors = append(descriptors, registration.Descriptor.Clone())
	}
	r.mu.RUnlock()
	sort.Slice(descriptors, func(i, j int) bool {
		if descriptors[i].Name == descriptors[j].Name {
			return descriptors[i].Version < descriptors[j].Version
		}
		return descriptors[i].Name < descriptors[j].Name
	})
	return descriptors
}

type RuntimeFailure struct {
	Code      string
	Message   string
	Retryable bool
	Cause     error
}

func (e *RuntimeFailure) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return e.Code
}

func (e *RuntimeFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func NewRuntimeFailure(code, message string, retryable bool, cause error) *RuntimeFailure {
	return &RuntimeFailure{
		Code: strings.TrimSpace(code), Message: strings.TrimSpace(message),
		Retryable: retryable, Cause: cause,
	}
}

func TemporalWorkflowIdentity(commandID, itemKey string, actionVersion int) (string, error) {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return "", fmt.Errorf("command ID is required")
	}
	if actionVersion < 1 {
		return "", fmt.Errorf("action version must be positive")
	}
	normalizedItemKey := strings.TrimSpace(itemKey)
	if normalizedItemKey == "" {
		normalizedItemKey = "command"
	}
	hash := sha256.Sum256([]byte(normalizedItemKey))
	return fmt.Sprintf("pc-v1-%s-a%d-%s", commandID, actionVersion, hex.EncodeToString(hash[:8])), nil
}

func runtimeHandlerKey(actionName string, actionVersion int) string {
	return fmt.Sprintf("%s@%d", strings.TrimSpace(actionName), actionVersion)
}
