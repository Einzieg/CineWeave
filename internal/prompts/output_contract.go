package prompts

import (
	"encoding/json"
	"strings"
)

// WithOutputContract appends the active prompt version's JSON Schema to the
// rendered text so every caller enforces the same structured-output contract.
func WithOutputContract(rendered RenderedPrompt) RenderedPrompt {
	var metadata struct {
		OutputContract json.RawMessage `json:"outputContract"`
	}
	if len(rendered.Metadata) == 0 ||
		json.Unmarshal(rendered.Metadata, &metadata) != nil ||
		len(metadata.OutputContract) == 0 {
		return rendered
	}
	var contract any
	if json.Unmarshal(metadata.OutputContract, &contract) != nil {
		return rendered
	}
	encoded, err := json.Marshal(contract)
	if err != nil {
		return rendered
	}
	rendered.RenderedText = strings.TrimSpace(rendered.RenderedText) +
		"\n\n输出必须严格匹配以下 JSON Schema，不得增加未声明字段，也不得改变字段类型：\n" +
		string(encoded)
	rendered.RenderedHash = HashText(rendered.RenderedText)
	return rendered
}
