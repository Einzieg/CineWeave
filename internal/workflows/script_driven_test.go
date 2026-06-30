package workflows

import (
	"encoding/json"
	"testing"
)

func TestNormalizeScriptAssetExtraction(t *testing.T) {
	assets, err := NormalizeScriptAssetExtraction(`{
	  "assets": [
	    {"assetType":"role","name":"林初","description":"摄影师","visualTraits":{"coat":"light"}},
	    {"assetType":"scene","name":"清晨火车站","description":"薄雾站台"},
	    {"assetType":"tool","name":"旧相机","description":"银黑色相机"},
	    {"assetType":"character","name":"林初","description":"duplicate"}
	  ]
	}`)
	if err != nil {
		t.Fatalf("NormalizeScriptAssetExtraction: %v", err)
	}
	if len(assets) != 3 {
		t.Fatalf("assets len = %d, want 3: %+v", len(assets), assets)
	}
	if assets[0].AssetType != "character" || assets[2].AssetType != "prop" {
		t.Fatalf("asset types = %+v", assets)
	}
	if !json.Valid(assets[0].VisualTraits) {
		t.Fatalf("visual traits not JSON: %s", assets[0].VisualTraits)
	}
}

func TestNormalizeShotAssetRequirements(t *testing.T) {
	requirements := NormalizeShotAssetRequirements(json.RawMessage(`{
	  "shots": [
	    {
	      "shotNo": 2,
	      "assetRequirements": [
	        {
	          "assetName": "林初",
	          "assetType": "role",
	          "costume": "浅色风衣",
	          "pose": "侧身站立",
	          "expression": "安静",
	          "action": "举起旧相机"
	        },
	        {
	          "assetName": "清晨火车站",
	          "assetType": "scene",
	          "sceneState": "逆光更强"
	        }
	      ]
	    }
	  ]
	}`))
	if len(requirements) != 2 {
		t.Fatalf("requirements len = %d, want 2", len(requirements))
	}
	if requirements[0].ShotNo != 2 || requirements[0].AssetType != "character" || requirements[0].RequirementType != "character_appearance" {
		t.Fatalf("character requirement = %+v", requirements[0])
	}
	if requirements[1].AssetType != "scene" || requirements[1].RequirementType != "scene_variant" {
		t.Fatalf("scene requirement = %+v", requirements[1])
	}
}

func TestResolveSourceToScriptOptions(t *testing.T) {
	options := resolveSourceToScriptOptions(json.RawMessage(`{
	  "sourceId": "  source-1  ",
	  "instruction": "  adapt the first chapter  ",
	  "title": "  Pilot Script  "
	}`))
	if options.SourceID != "source-1" || options.Instruction != "adapt the first chapter" || options.Title != "Pilot Script" {
		t.Fatalf("options = %+v", options)
	}
}
