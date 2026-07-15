package assetprompts

import "testing"

func TestValidateGeneratedCardStyleRejectsCrossFamilyPrompts(t *testing.T) {
	tests := []struct {
		name        string
		style       string
		base        string
		consistency string
		wantError   bool
	}{
		{name: "3d accepts 3d", style: "3d_chinese_traditional", base: "国风3D场景设定图，高精度建模", consistency: "保持3D渲染", wantError: false},
		{name: "3d rejects live action", style: "3d_chinese_traditional", base: "真人都市场景全景摄影，35mm全画幅摄影质感", consistency: "保持真实摄影画质", wantError: true},
		{name: "2d rejects 3d", style: "2d_90s_japanese_anime", base: "3D渲染角色模型", consistency: "保持3D rendered风格", wantError: true},
		{name: "live action rejects animation", style: "realpeople_modern_city", base: "二次元赛璐璐角色设定", consistency: "保持cel shading", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateGeneratedCardStyle(test.style, test.base, test.consistency)
			if (err != nil) != test.wantError {
				t.Fatalf("ValidateGeneratedCardStyle() error = %v, wantError=%v", err, test.wantError)
			}
		})
	}
}
