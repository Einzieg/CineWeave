package api

import (
	"encoding/json"
	"testing"

	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/stretchr/testify/require"
)

func TestMergeCommerceScriptUnitDefaultsPreservesUnrelatedSettings(t *testing.T) {
	settings, err := mergeCommerceScriptUnitDefaults(json.RawMessage(`{"custom":{"enabled":true}}`), commercepkg.ScriptUnitDefaults{
		TargetDurationSeconds: 60,
		TargetPlatform:        "tiktok",
		LanguageMode:          "explicit",
		TargetLanguage:        stringPointer("en-US"),
	})
	require.NoError(t, err)
	var document map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(settings, &document))
	require.JSONEq(t, `{"enabled":true}`, string(document["custom"]))
	defaults, err := commerceScriptUnitDefaultsFromSettings(settings)
	require.NoError(t, err)
	require.Equal(t, 60, defaults.TargetDurationSeconds)
	require.Equal(t, "tiktok", defaults.TargetPlatform)
	require.Equal(t, "explicit", defaults.LanguageMode)
	require.Equal(t, "en-US", dereferenceString(defaults.TargetLanguage))
}

func TestCommerceScriptUnitDefaultsFallbackDoesNotMutateSettings(t *testing.T) {
	defaults, err := commerceScriptUnitDefaultsFromSettings(json.RawMessage(`{"legacy":"preserved"}`))
	require.NoError(t, err)
	require.Equal(t, defaultCommerceScriptUnitDefaults(), defaults)
}

func TestNormalizedCommerceScriptUnitDefaultsCanonicalizesLocale(t *testing.T) {
	defaults, err := normalizedCommerceScriptUnitDefaults(commercepkg.ScriptUnitDefaults{
		TargetDurationSeconds: 30,
		TargetPlatform:        " tiktok ",
		LanguageMode:          " EXPLICIT ",
		TargetLanguage:        stringPointer("zh-cn"),
	})
	require.NoError(t, err)
	require.Equal(t, "tiktok", defaults.TargetPlatform)
	require.Equal(t, "explicit", defaults.LanguageMode)
	require.Equal(t, "zh-CN", dereferenceString(defaults.TargetLanguage))
}
