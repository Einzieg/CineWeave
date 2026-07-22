package main

import (
	"testing"

	"github.com/Einzieg/cineweave/internal/workflows"
	"go.temporal.io/sdk/activity"
)

type recordingScriptActivityRegistrar struct {
	names []string
}

func (r *recordingScriptActivityRegistrar) RegisterActivityWithOptions(_ interface{}, options activity.RegisterOptions) {
	r.names = append(r.names, options.Name)
}

func TestRegisterShotAnchorActivities(t *testing.T) {
	registrar := &recordingScriptActivityRegistrar{}
	registerShotAnchorActivities(registrar, workflows.Activities{})
	if len(registrar.names) != 1 || registrar.names[0] != "ResolveShotAnchorWorkItems" {
		t.Fatalf("registered activities = %v", registrar.names)
	}
}
