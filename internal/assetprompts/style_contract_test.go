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

func TestValidateCanonicalAssetBaselineRejectsTransientCharacterInjuries(t *testing.T) {
	tests := []struct {
		name      string
		assetType string
		base      string
		stable    string
		wantError bool
	}{
		{name: "neutral character", assetType: "character", base: "国风3D成年男性角色四视图，素色长袍", stable: "固定面容、体型和基础服装", wantError: false},
		{name: "explicitly excluded injuries", assetType: "character", base: "国风3D成年男性，无血迹、无流血、无开放伤口、无战损", stable: "核心资产不固化血迹、流血、开放伤口等剧情瞬时状态", wantError: false},
		{name: "blood soaked character", assetType: "character", base: "国风3D成年男性，残袍被鲜血浸透", stable: "保持浑身浴血与遍体伤口", wantError: true},
		{name: "scene may contain old blood trace", assetType: "scene", base: "风化山巅地表有陈旧血迹", stable: "固定岩石结构", wantError: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateCanonicalAssetBaseline(test.assetType, test.base, test.stable)
			if (err != nil) != test.wantError {
				t.Fatalf("ValidateCanonicalAssetBaseline() error = %v, wantError=%v", err, test.wantError)
			}
		})
	}
}
