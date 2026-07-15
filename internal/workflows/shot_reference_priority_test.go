package workflows

import "testing"

func TestShotImageReferencePriorityPrefersNamedLeaderOverCrowdAndScene(t *testing.T) {
	title := "首领怒喝"
	body := "正道群雄围困山巅，正道首领上前怒斥方源。"
	leader, _ := shotImageReferencePriority(title, body, "character", "正道首领", "正道首领乙", "character_appearance")
	crowd, _ := shotImageReferencePriority(title, body, "character", "正道群雄", "背景群像", "character_appearance")
	scene, _ := shotImageReferencePriority(title, body, "scene", "绝境山巅", "环境", "scene_environment")

	if leader <= crowd || leader <= scene {
		t.Fatalf("priority leader=%d crowd=%d scene=%d, want leader highest", leader, crowd, scene)
	}
}

func TestContainsReferenceNameFragmentMatchesChineseRoleName(t *testing.T) {
	if !containsReferenceNameFragment("首领怒喝", "正道首领") {
		t.Fatal("expected the shared 首领 fragment to match")
	}
	if containsReferenceNameFragment("山巅风云", "正道首领") {
		t.Fatal("unexpected unrelated fragment match")
	}
}
