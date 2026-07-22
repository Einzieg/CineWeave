package assetprompts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildCanonicalCardSourceRemovesTransientCharacterState(t *testing.T) {
	source := BuildCanonicalCardSource(
		"character",
		"重生前的方源，在魔窟外山巅被正道群雄围困",
		json.RawMessage(`{"age":"成年","face":"脸色因失血苍白","hair":"部分贴在染血面颊和颈侧","state":"遍体伤口、浑身浴血","clothing":"残破的碧绿大袍，被鲜血浸透"}`),
		"山巅血战，遍体伤口",
	)
	combined := source.Description + "\n" + string(source.VisualTraits) + "\n" + source.SceneContext
	for _, forbidden := range []string{"围困", "失血", "染血", "伤口", "浴血", "鲜血", `"state"`} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("canonical source retained transient term %q: %s", forbidden, combined)
		}
	}
	for _, required := range []string{"重生前的方源", "成年", "碧绿色古代长袍"} {
		if !strings.Contains(combined, required) {
			t.Fatalf("canonical source lost stable term %q: %s", required, combined)
		}
	}
	if source.SceneContext != "" {
		t.Fatalf("character scene context was retained: %q", source.SceneContext)
	}
}

func TestBuildCanonicalCardSourceKeepsSceneContextForNonCharacters(t *testing.T) {
	source := BuildCanonicalCardSource("scene", "山巅场景", json.RawMessage(`{"weather":"黄昏"}`), "相关剧本场景")
	if source.SceneContext != "相关剧本场景" || !strings.Contains(string(source.VisualTraits), "黄昏") {
		t.Fatalf("non-character source was changed: %+v", source)
	}
}
