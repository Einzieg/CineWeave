package workflows

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func TestCommerceRuntimeRegistrationAcceptsProductionSignatures(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	activityEnvironment := suite.NewTestActivityEnvironment()
	workflowEnvironment := suite.NewTestWorkflowEnvironment()

	assertDoesNotPanic(t, "Commerce activities", func() {
		RegisterCommerceActivities(activityEnvironment, CommerceActivities{})
	})
	assertDoesNotPanic(t, "Commerce workflows", func() {
		RegisterCommerceWorkflows(workflowEnvironment)
	})
}

func assertDoesNotPanic(t *testing.T, name string, register func()) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("%s registration panicked: %v", name, recovered)
		}
	}()
	register()
}
