package api

import (
	"strings"
	"testing"

	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
)

func TestCommerceScriptOrdinalFromGoal(t *testing.T) {
	tests := []struct {
		goal string
		want int
	}{
		{goal: "把第二条脚本的场景换五个版本", want: 2},
		{goal: "生成第 12 条广告脚本的视频", want: 12},
		{goal: "修改第２个脚本", want: 2},
		{goal: "第 2 条 改变人物和场景，改3个版本", want: 2},
		{goal: "第3条，换一个开场", want: 3},
		{goal: "修改标题为第二条街的脚本", want: 0},
		{goal: "使用第2条卖点", want: 0},
	}
	for _, test := range tests {
		if got := commerceScriptOrdinalFromGoal(test.goal); got != test.want {
			t.Fatalf("commerceScriptOrdinalFromGoal(%q) = %d, want %d", test.goal, got, test.want)
		}
	}
}

func TestResolveCommerceAgentScriptSelectionReplacesInventedID(t *testing.T) {
	scripts := commercepkg.ScriptUnitList{
		ScriptUnitsRevision: 9,
		Items: []commercepkg.ScriptUnit{
			{ID: "11111111-1111-4111-8111-111111111111", Title: "后创建但排在第一"},
			{ID: "22222222-2222-4222-8222-222222222222", Title: "标题不含序号"},
		},
	}
	args := map[string]any{
		"sourceScriptUnitId": "22222222-2222-4222-8222-111111111111",
	}
	resolved, err := resolveCommerceAgentScriptSelection(args, "sourceScriptUnitId", 2, scripts)
	if err != nil {
		t.Fatalf("resolve stable ordinal: %v", err)
	}
	if resolved.ScriptUnitID != scripts.Items[1].ID || resolved.StableOrdinal != 2 || resolved.ScriptUnitsRevision != 9 {
		t.Fatalf("resolved selection = %+v", resolved)
	}
	if got := stringValueFromAny(args["sourceScriptUnitId"]); got != scripts.Items[1].ID {
		t.Fatalf("resolved sourceScriptUnitId = %q", got)
	}
	if got := agentIntArg(args, "stableOrdinal", 0, 0, 100); got != 2 {
		t.Fatalf("stableOrdinal = %d", got)
	}
	if got := agentInt64Value(args["expectedScriptUnitsRevision"]); got != 9 {
		t.Fatalf("expectedScriptUnitsRevision = %d", got)
	}
}

func TestResolveCommerceAgentScriptSelectionRejectsChangedListRevision(t *testing.T) {
	args := map[string]any{
		"stableOrdinal":               2,
		"expectedScriptUnitsRevision": 8,
	}
	_, err := resolveCommerceAgentScriptSelection(args, "scriptUnitId", 2, commercepkg.ScriptUnitList{
		ScriptUnitsRevision: 9,
		Items: []commercepkg.ScriptUnit{
			{ID: "11111111-1111-4111-8111-111111111111"},
			{ID: "22222222-2222-4222-8222-222222222222"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "从 8 变为 9") {
		t.Fatalf("stale selection error = %v", err)
	}
}

func TestResolveCommerceAgentScriptSelectionRequiresExistingOrdinal(t *testing.T) {
	_, err := resolveCommerceAgentScriptSelection(
		map[string]any{"stableOrdinal": 2},
		"scriptUnitId",
		0,
		commercepkg.ScriptUnitList{
			ScriptUnitsRevision: 3,
			Items:               []commercepkg.ScriptUnit{{ID: "first"}},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "第 2 条") {
		t.Fatalf("missing stable ordinal error = %v", err)
	}
}

func TestCommerceScriptReviseUsesStableScriptIdentity(t *testing.T) {
	if got := commerceScriptTargetArgument["commerce.script.revise"]; got != "scriptUnitId" {
		t.Fatalf("commerce.script.revise identity argument = %q", got)
	}
}
