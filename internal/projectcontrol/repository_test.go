package projectcontrol

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestCanonicalObjectIsStableAndRejectsNonObjects(t *testing.T) {
	first, firstHash, err := canonicalObject(json.RawMessage(`{"z":2,"a":1}`), 1024)
	if err != nil {
		t.Fatalf("canonicalize first object: %v", err)
	}
	second, secondHash, err := canonicalObject(json.RawMessage("{\n  \"a\": 1, \"z\": 2\n}"), 1024)
	if err != nil {
		t.Fatalf("canonicalize second object: %v", err)
	}
	if string(first) != string(second) || firstHash != secondHash {
		t.Fatalf("canonical objects differ: %s/%s versus %s/%s", first, firstHash, second, secondHash)
	}
	for _, raw := range []json.RawMessage{json.RawMessage(`null`), json.RawMessage(`[]`), json.RawMessage(`"value"`)} {
		if _, _, err := canonicalObject(raw, 1024); err == nil {
			t.Fatalf("canonicalObject(%s) succeeded, want object error", raw)
		}
	}
}

func TestCommandRequestHashIncludesFrozenItems(t *testing.T) {
	input := json.RawMessage(`{"projectId":"11111111-1111-1111-1111-111111111111"}`)
	base := []CreateCommandItem{{
		ItemKey: "episode:1", TargetType: "source_chapter",
		TargetID: "22222222-2222-2222-2222-222222222222", Input: json.RawMessage(`{"revision":1}`),
	}}
	first, err := commandRequestHash(input, base)
	if err != nil {
		t.Fatalf("hash base request: %v", err)
	}
	changed := append([]CreateCommandItem(nil), base...)
	changed[0].Input = json.RawMessage(`{"revision":2}`)
	second, err := commandRequestHash(input, changed)
	if err != nil {
		t.Fatalf("hash changed request: %v", err)
	}
	if first == second {
		t.Fatal("command request hash did not include item input")
	}
}

func TestCreateCommandValidationRequiresControllerIdentity(t *testing.T) {
	descriptor := testCommandDescriptor()
	request := CreateCommand{
		OrganizationID: "organization", ActorUserID: "user", ControllerType: ControllerCodexMCP,
		Descriptor: descriptor, IdempotencyKey: "key", Input: json.RawMessage(`{}`),
	}
	if err := request.Validate(); err == nil {
		t.Fatal("Codex command without control key succeeded")
	}
	request.ControlKeyID = "control-key"
	if err := request.Validate(); err != nil {
		t.Fatalf("valid Codex command rejected: %v", err)
	}
	request.Items = []CreateCommandItem{
		{ItemKey: "same", TargetType: "asset"},
		{ItemKey: "same", TargetType: "asset"},
	}
	if err := request.Validate(); err == nil {
		t.Fatal("duplicate command item key succeeded")
	}
}

func TestCommandErrorsRemainClassifiable(t *testing.T) {
	err := errors.Join(ErrIdempotencyConflict, errors.New("different input"))
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal("idempotency conflict cannot be classified")
	}
}

func testCommandDescriptor() Descriptor {
	return Descriptor{
		Name: "test.action", Version: 1, Label: "测试动作", Summary: "测试动作",
		Risk: RiskWrite, Scope: ScopeProject,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		Effects:      Effects{WritesProject: true}, ReadOnly: false, Destructive: false,
		Idempotent: true, ExecutionMode: ExecutionModeAsyncCommand,
		ActivityVisibility: ActivityVisibilityPrimary, ExportToMCP: true,
	}
}
