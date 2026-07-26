package commerce

import "testing"

func TestIncompleteSetupStateError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		state       string
		lastMessage string
		wantCode    string
		wantMessage string
	}{
		{
			name:        "legacy confirmation state is recoverable",
			state:       "waiting_user_confirmation",
			wantCode:    CodeSetupIncomplete,
			wantMessage: "项目准备正在完成语言识别，请稍候或重试项目准备",
		},
		{
			name:        "active setup",
			state:       "localizing",
			wantCode:    CodeSetupIncomplete,
			wantMessage: "项目准备流程尚未完成，请先在商品与脚本页面等待或完成当前步骤",
		},
		{
			name:        "failed setup keeps the actionable cause",
			state:       "failed",
			lastMessage: "供应商请求超时",
			wantCode:    CodeSetupIncomplete,
			wantMessage: "项目准备失败：供应商请求超时",
		},
		{
			name:        "completed setup without generation is inconsistent",
			state:       "completed",
			wantCode:    CodeProjectNotConfigured,
			wantMessage: "项目准备已完成但生产配置缺失，请重试项目准备或联系管理员",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := incompleteSetupStateError(test.state, "", test.lastMessage)
			if got.Code != test.wantCode {
				t.Fatalf("code = %q, want %q", got.Code, test.wantCode)
			}
			if got.Message != test.wantMessage {
				t.Fatalf("message = %q, want %q", got.Message, test.wantMessage)
			}
			if got.Details["setupState"] != test.state {
				t.Fatalf("setupState = %#v, want %q", got.Details["setupState"], test.state)
			}
		})
	}
}
