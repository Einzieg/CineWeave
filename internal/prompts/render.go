package prompts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

var (
	templateExprPattern = regexp.MustCompile(`{{\s*([^{}]+?)\s*}}`)
	dotPathPattern      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
)

func Render(prompt ResolvedPrompt, variables map[string]any) (RenderedPrompt, error) {
	if strings.TrimSpace(prompt.VersionID) == "" {
		return RenderedPrompt{}, Error{Code: CodePromptVersionNotFound, Message: "prompt version id is required"}
	}
	rendered := templateExprPattern.ReplaceAllStringFunc(prompt.Content, func(match string) string {
		parts := templateExprPattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return ""
		}
		path := strings.TrimSpace(parts[1])
		if !dotPathPattern.MatchString(path) {
			return ""
		}
		value, ok := lookupPath(variables, strings.Split(path, "."))
		if !ok {
			return ""
		}
		return valueToString(value)
	})
	return RenderedPrompt{
		PromptVersionID: prompt.VersionID,
		TemplateKey:     prompt.TemplateKey,
		RenderedText:    rendered,
		RenderedHash:    HashText(rendered),
		ContentHash:     prompt.ContentHash,
		Source:          prompt.Source,
	}, nil
}

func lookupPath(root any, parts []string) (any, bool) {
	current := root
	for _, part := range parts {
		value := reflect.ValueOf(current)
		for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
			if value.IsNil() {
				return nil, false
			}
			value = value.Elem()
		}
		switch value.Kind() {
		case reflect.Map:
			if value.Type().Key().Kind() != reflect.String {
				return nil, false
			}
			next := value.MapIndex(reflect.ValueOf(part))
			if !next.IsValid() {
				return nil, false
			}
			current = next.Interface()
		case reflect.Struct:
			next := value.FieldByName(part)
			if !next.IsValid() {
				next = fieldByJSONTag(value, part)
			}
			if !next.IsValid() || !next.CanInterface() {
				return nil, false
			}
			current = next.Interface()
		default:
			return nil, false
		}
	}
	return current, true
}

func fieldByJSONTag(value reflect.Value, name string) reflect.Value {
	valueType := value.Type()
	for i := 0; i < value.NumField(); i++ {
		field := valueType.Field(i)
		tagName := strings.Split(field.Tag.Get("json"), ",")[0]
		if tagName == name {
			return value.Field(i)
		}
	}
	return reflect.Value{}
}

func valueToString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case bool:
		return strconv.FormatBool(typed)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(typed)
	case json.Number:
		return typed.String()
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(raw)
	}
}

func HashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
