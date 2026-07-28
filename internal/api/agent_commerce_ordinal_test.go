package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/agent"
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
		{goal: "修改标题为第二条街的脚本", want: 0},
	}
	for _, test := range tests {
		if got := commerceScriptOrdinalFromGoal(test.goal); got != test.want {
			t.Fatalf("commerceScriptOrdinalFromGoal(%q) = %d, want %d", test.goal, got, test.want)
		}
	}
}

func TestValidateCommerceScriptOrdinalPlanUsesStableListIdentity(t *testing.T) {
	scripts := []commercepkg.ScriptUnit{
		{ID: "11111111-1111-4111-8111-111111111111", Title: "后创建但排在第一"},
		{ID: "22222222-2222-4222-8222-222222222222", Title: "标题不含序号"},
	}
	correct := agent.Plan{Steps: []agent.PlanStep{{
		Tool: "commerce.script.derive.preview",
		Args: json.RawMessage(`{
			"sourceScriptUnitId":"22222222-2222-4222-8222-222222222222",
			"count":5,
			"dimension":"scene",
			"instruction":"替换场景"
		}`),
	}}}
	if err := validateCommerceScriptOrdinalPlan(correct, 2, scripts); err != nil {
		t.Fatalf("correct stable ordinal plan rejected: %v", err)
	}

	wrong := correct
	wrong.Steps = append([]agent.PlanStep(nil), correct.Steps...)
	wrong.Steps[0].Args = json.RawMessage(`{
		"sourceScriptUnitId":"11111111-1111-4111-8111-111111111111",
		"count":5,
		"dimension":"scene",
		"instruction":"替换场景"
	}`)
	err := validateCommerceScriptOrdinalPlan(wrong, 2, scripts)
	if err == nil || !strings.Contains(err.Error(), scripts[1].ID) {
		t.Fatalf("wrong stable ordinal plan error = %v", err)
	}
}

func TestValidateCommerceScriptOrdinalPlanRequiresQuestionWhenMissing(t *testing.T) {
	targeted := agent.Plan{Steps: []agent.PlanStep{{
		Tool: "commerce.video.generate",
		Args: json.RawMessage(`{"scriptUnitId":"22222222-2222-4222-8222-222222222222"}`),
	}}}
	if err := validateCommerceScriptOrdinalPlan(targeted, 2, []commercepkg.ScriptUnit{{ID: "first"}}); err == nil {
		t.Fatal("missing stable ordinal must reject targeted execution")
	}

	ask := agent.Plan{Steps: []agent.PlanStep{{
		Tool: "agent.ask_user",
		Args: json.RawMessage(`{
			"question":"当前没有第二条活动脚本，请选择下一步",
			"allowCustom":true,
			"options":[
				{"id":"first","label":"使用第一条脚本","description":"改用当前第一条活动脚本"},
				{"id":"cancel","label":"取消","description":"不执行本次任务"}
			]
		}`),
	}}}
	if err := validateCommerceScriptOrdinalPlan(ask, 2, []commercepkg.ScriptUnit{{ID: "first"}}); err != nil {
		t.Fatalf("ambiguity question rejected: %v", err)
	}
}
