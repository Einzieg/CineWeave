package provider

import (
	"encoding/json"
	"testing"
)

func TestGatewayVideoRecoveryTaskInputAndIdentity(t *testing.T) {
	input := withGatewayVideoRecoverySource(json.RawMessage(`{"prompt":"keep me"}`), "source-task")
	if got := gatewayVideoRecoverySourceTaskID(input); got != "source-task" {
		t.Fatalf("recovery source task = %q, want source-task", got)
	}
	var decoded map[string]any
	if err := json.Unmarshal(input, &decoded); err != nil {
		t.Fatalf("decode recovery input: %v", err)
	}
	if decoded["prompt"] != "keep me" {
		t.Fatalf("prompt was not preserved: %#v", decoded)
	}

	first := gatewayVideoRecoveryTaskExternalID("source-task", "node-run", 2)
	second := gatewayVideoRecoveryTaskExternalID("source-task", "node-run", 2)
	otherAttempt := gatewayVideoRecoveryTaskExternalID("source-task", "node-run", 3)
	if first != second || first == otherAttempt {
		t.Fatalf("recovery task identity is not deterministic per node attempt: %q %q %q", first, second, otherAttempt)
	}
}

func TestGatewayVideoTaskRecordingMode(t *testing.T) {
	taskType, callMode, taskMode, callStatus := gatewayVideoTaskRecordingMode(json.RawMessage(`{"prompt":"normal"}`))
	if taskType != TaskTypeVideoCreateTask || callMode != "async_create" || taskMode != "async_polling" || callStatus != "" {
		t.Fatalf("normal recording mode = %q %q %q %q", taskType, callMode, taskMode, callStatus)
	}

	recovery := withGatewayVideoRecoverySource(json.RawMessage(`{"prompt":"recover"}`), "source-task")
	taskType, callMode, taskMode, callStatus = gatewayVideoTaskRecordingMode(recovery)
	if taskType != TaskTypeVideoPollTask || callMode != "async_poll" || taskMode != "media_recovery" || callStatus != "succeeded" {
		t.Fatalf("recovery recording mode = %q %q %q %q", taskType, callMode, taskMode, callStatus)
	}
}
