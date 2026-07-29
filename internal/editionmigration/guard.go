package editionmigration

import (
	"fmt"
	"strings"
	"unicode"
)

var ddlKeywords = map[string]struct{}{
	"alter":    {},
	"comment":  {},
	"create":   {},
	"drop":     {},
	"grant":    {},
	"revoke":   {},
	"truncate": {},
}

type ddlTarget struct {
	schema   string
	object   string
	isSchema bool
}

// ValidateCommercialMigration enforces the public DDL ownership contract.
// Commercial SQL may create or mutate only objects in
// cineweave_commercial. The Commercial ledger schema is runner-owned, and
// unqualified DDL is rejected so search_path cannot change ownership.
func ValidateCommercialMigration(name string, content []byte) error {
	tokens := executableSQLTokens(string(content))
	for _, statement := range splitStatements(tokens) {
		if len(statement) == 0 {
			continue
		}
		if hasDynamicExecute(statement) {
			return fmt.Errorf("migration %q uses dynamic EXECUTE; Commercial DDL must be statically auditable", name)
		}
		if containsTokenSequence(statement, "set", "search_path") {
			return fmt.Errorf("migration %q changes search_path; Commercial DDL targets must be schema-qualified", name)
		}

		command := firstDDLKeyword(statement)
		if command < 0 {
			continue
		}
		targets, err := parseDDLTargets(statement[command:])
		if err != nil {
			return fmt.Errorf("migration %q: %w", name, err)
		}
		if len(targets) == 0 {
			return fmt.Errorf("migration %q contains DDL that the owner guard cannot classify", name)
		}
		for _, target := range targets {
			if target.isSchema {
				if target.object != CommercialObjectSchema {
					return fmt.Errorf(
						"migration %q targets schema %q owned outside Commercial",
						name,
						target.object,
					)
				}
				continue
			}
			if target.schema == "" {
				return fmt.Errorf(
					"migration %q has unqualified DDL target %q; Commercial targets must use %s.<object>",
					name,
					target.object,
					CommercialObjectSchema,
				)
			}
			if target.schema != CommercialObjectSchema {
				return fmt.Errorf(
					"migration %q targets %s.%s owned outside Commercial",
					name,
					target.schema,
					target.object,
				)
			}
		}
	}
	return nil
}

func parseDDLTargets(tokens []string) ([]ddlTarget, error) {
	if len(tokens) == 0 {
		return nil, nil
	}
	switch tokens[0] {
	case "create":
		return parseCreateTargets(tokens)
	case "alter":
		return parseAlterTargets(tokens)
	case "drop":
		return parseDropTargets(tokens)
	case "truncate":
		return parseTruncateTargets(tokens)
	case "comment":
		return parseCommentTargets(tokens)
	case "grant", "revoke":
		return nil, fmt.Errorf("%s statements are not allowed in Commercial migrations", strings.ToUpper(tokens[0]))
	default:
		return nil, nil
	}
}

func parseCreateTargets(tokens []string) ([]ddlTarget, error) {
	index := 1
	index = skipTokens(tokens, index, "or", "replace")
	for index < len(tokens) && oneOf(tokens[index], "temporary", "temp", "unlogged", "unique") {
		index++
	}
	if index >= len(tokens) {
		return nil, fmt.Errorf("incomplete CREATE statement")
	}
	kind := tokens[index]
	index++
	if kind == "materialized" {
		if index >= len(tokens) || tokens[index] != "view" {
			return nil, fmt.Errorf("unsupported CREATE MATERIALIZED statement")
		}
		kind = "view"
		index++
	}
	switch kind {
	case "index":
		on := tokenIndex(tokens, index, "on")
		if on < 0 {
			return nil, fmt.Errorf("CREATE INDEX has no ON target")
		}
		index = on + 1
		if index < len(tokens) && tokens[index] == "only" {
			index++
		}
		target, _, err := parseQualifiedTarget(tokens, index, false)
		return targetSlice(target), err
	case "trigger", "constraint":
		on := tokenIndex(tokens, index, "on")
		if on < 0 {
			return nil, fmt.Errorf("CREATE %s has no ON target", strings.ToUpper(kind))
		}
		target, _, err := parseQualifiedTarget(tokens, on+1, false)
		return targetSlice(target), err
	case "policy", "rule":
		on := tokenIndex(tokens, index, "on")
		if on < 0 {
			return nil, fmt.Errorf("CREATE %s has no ON target", strings.ToUpper(kind))
		}
		target, _, err := parseQualifiedTarget(tokens, on+1, false)
		return targetSlice(target), err
	case "schema":
		index = skipOptionalExistence(tokens, index)
		target, _, err := parseQualifiedTarget(tokens, index, true)
		return targetSlice(target), err
	case "table", "view", "sequence", "function", "procedure", "type", "domain", "collation":
		index = skipOptionalExistence(tokens, index)
		target, _, err := parseQualifiedTarget(tokens, index, false)
		return targetSlice(target), err
	default:
		return nil, fmt.Errorf("unsupported CREATE target type %q", kind)
	}
}

func parseAlterTargets(tokens []string) ([]ddlTarget, error) {
	if len(tokens) < 2 {
		return nil, fmt.Errorf("incomplete ALTER statement")
	}
	index := 1
	kind := tokens[index]
	index++
	if kind == "materialized" {
		if index >= len(tokens) || tokens[index] != "view" {
			return nil, fmt.Errorf("unsupported ALTER MATERIALIZED statement")
		}
		kind = "view"
		index++
	}
	if kind == "policy" || kind == "rule" || kind == "trigger" {
		on := tokenIndex(tokens, index, "on")
		if on < 0 {
			return nil, fmt.Errorf("ALTER %s has no ON target", strings.ToUpper(kind))
		}
		target, _, err := parseQualifiedTarget(tokens, on+1, false)
		return targetSlice(target), err
	}
	if !oneOf(kind, "table", "view", "sequence", "function", "procedure", "type", "domain", "schema", "index") {
		return nil, fmt.Errorf("unsupported ALTER target type %q", kind)
	}
	index = skipOptionalExists(tokens, index)
	if index < len(tokens) && tokens[index] == "only" {
		index++
	}
	target, _, err := parseQualifiedTarget(tokens, index, kind == "schema")
	return targetSlice(target), err
}

func parseDropTargets(tokens []string) ([]ddlTarget, error) {
	if len(tokens) < 2 {
		return nil, fmt.Errorf("incomplete DROP statement")
	}
	index := 1
	kind := tokens[index]
	index++
	if kind == "materialized" {
		if index >= len(tokens) || tokens[index] != "view" {
			return nil, fmt.Errorf("unsupported DROP MATERIALIZED statement")
		}
		kind = "view"
		index++
	}
	if kind == "trigger" || kind == "policy" || kind == "rule" || kind == "constraint" {
		on := lastTokenIndex(tokens, "on")
		if on < 0 {
			return nil, fmt.Errorf("DROP %s has no ON target", strings.ToUpper(kind))
		}
		target, _, err := parseQualifiedTarget(tokens, on+1, false)
		return targetSlice(target), err
	}
	if !oneOf(kind, "table", "view", "sequence", "function", "procedure", "type", "domain", "schema", "index", "collation") {
		return nil, fmt.Errorf("unsupported DROP target type %q", kind)
	}
	index = skipOptionalExists(tokens, index)
	return parseTargetList(tokens, index, kind == "schema")
}

func parseTruncateTargets(tokens []string) ([]ddlTarget, error) {
	index := 1
	if index < len(tokens) && tokens[index] == "table" {
		index++
	}
	if index < len(tokens) && tokens[index] == "only" {
		index++
	}
	return parseTargetList(tokens, index, false)
}

func parseCommentTargets(tokens []string) ([]ddlTarget, error) {
	if len(tokens) < 4 || tokens[1] != "on" {
		return nil, fmt.Errorf("unsupported COMMENT statement")
	}
	kind := tokens[2]
	index := 3
	if oneOf(kind, "constraint", "trigger", "policy", "rule") {
		on := tokenIndex(tokens, index, "on")
		if on < 0 {
			return nil, fmt.Errorf("COMMENT ON %s has no owner target", strings.ToUpper(kind))
		}
		index = on + 1
	}
	target, _, err := parseQualifiedTarget(tokens, index, kind == "schema")
	return targetSlice(target), err
}

func parseTargetList(tokens []string, index int, isSchema bool) ([]ddlTarget, error) {
	var targets []ddlTarget
	for index < len(tokens) {
		if oneOf(tokens[index], "cascade", "restrict") {
			break
		}
		if tokens[index] == "only" {
			index++
			continue
		}
		target, next, err := parseQualifiedTarget(tokens, index, isSchema)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
		index = next
		// Function signatures are irrelevant to ownership; skip to the next
		// top-level comma or statement option.
		depth := 0
		for index < len(tokens) {
			switch tokens[index] {
			case "(":
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
			case ",":
				if depth == 0 {
					index++
					goto nextTarget
				}
			case "cascade", "restrict":
				if depth == 0 {
					return targets, nil
				}
			}
			index++
		}
		break
	nextTarget:
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("DDL statement has no object target")
	}
	return targets, nil
}

func parseQualifiedTarget(tokens []string, index int, isSchema bool) (ddlTarget, int, error) {
	if index >= len(tokens) || !isIdentifierToken(tokens[index]) {
		return ddlTarget{}, index, fmt.Errorf("DDL target is missing")
	}
	first := tokens[index]
	index++
	if isSchema {
		return ddlTarget{object: first, isSchema: true}, index, nil
	}
	if index+1 < len(tokens) && tokens[index] == "." && isIdentifierToken(tokens[index+1]) {
		return ddlTarget{schema: first, object: tokens[index+1]}, index + 2, nil
	}
	return ddlTarget{object: first}, index, nil
}

func targetSlice(target ddlTarget) []ddlTarget {
	if target.object == "" {
		return nil
	}
	return []ddlTarget{target}
}

func splitStatements(tokens []string) [][]string {
	var statements [][]string
	start := 0
	for index, token := range tokens {
		if token != ";" {
			continue
		}
		if index > start {
			statements = append(statements, tokens[start:index])
		}
		start = index + 1
	}
	if start < len(tokens) {
		statements = append(statements, tokens[start:])
	}
	return statements
}

func firstDDLKeyword(tokens []string) int {
	for index, token := range tokens {
		if _, ok := ddlKeywords[token]; ok {
			return index
		}
	}
	return -1
}

func containsToken(tokens []string, expected string) bool {
	return tokenIndex(tokens, 0, expected) >= 0
}

func hasDynamicExecute(tokens []string) bool {
	for index, token := range tokens {
		if token != "execute" {
			continue
		}
		if index+1 < len(tokens) && oneOf(tokens[index+1], "function", "procedure") {
			continue
		}
		return true
	}
	return false
}

func containsTokenSequence(tokens []string, expected ...string) bool {
	if len(expected) == 0 || len(expected) > len(tokens) {
		return false
	}
	for index := 0; index <= len(tokens)-len(expected); index++ {
		match := true
		for offset, token := range expected {
			if tokens[index+offset] != token {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func tokenIndex(tokens []string, start int, expected string) int {
	for index := start; index < len(tokens); index++ {
		if tokens[index] == expected {
			return index
		}
	}
	return -1
}

func lastTokenIndex(tokens []string, expected string) int {
	for index := len(tokens) - 1; index >= 0; index-- {
		if tokens[index] == expected {
			return index
		}
	}
	return -1
}

func skipTokens(tokens []string, index int, expected ...string) int {
	if index+len(expected) > len(tokens) {
		return index
	}
	for offset, token := range expected {
		if tokens[index+offset] != token {
			return index
		}
	}
	return index + len(expected)
}

func skipOptionalExistence(tokens []string, index int) int {
	return skipTokens(tokens, index, "if", "not", "exists")
}

func skipOptionalExists(tokens []string, index int) int {
	return skipTokens(tokens, index, "if", "exists")
}

func oneOf(value string, expected ...string) bool {
	for _, candidate := range expected {
		if value == candidate {
			return true
		}
	}
	return false
}

func isIdentifierToken(token string) bool {
	if token == "" || token == "." || token == "," || token == "(" || token == ")" || token == ";" {
		return false
	}
	_, reserved := ddlKeywords[token]
	return !reserved && token != "if" && token != "exists" && token != "not" && token != "only"
}

func executableSQLTokens(content string) []string {
	var tokens []string
	for index := 0; index < len(content); {
		switch {
		case strings.HasPrefix(content[index:], "--"):
			if newline := strings.IndexByte(content[index:], '\n'); newline >= 0 {
				index += newline + 1
			} else {
				index = len(content)
			}
		case strings.HasPrefix(content[index:], "/*"):
			if end := strings.Index(content[index+2:], "*/"); end >= 0 {
				index += end + 4
			} else {
				index = len(content)
			}
		case content[index] == '\'':
			index = skipSingleQuoted(content, index)
		case content[index] == '"':
			var identifier string
			identifier, index = readQuotedIdentifier(content, index)
			if identifier != "" {
				tokens = append(tokens, strings.ToLower(identifier))
			}
		case content[index] == '$':
			tag, ok := dollarQuoteTag(content[index:])
			if !ok {
				index++
				continue
			}
			bodyStart := index + len(tag)
			bodyEnd := strings.Index(content[bodyStart:], tag)
			if bodyEnd < 0 {
				bodyEnd = len(content) - bodyStart
			}
			if dollarBodyExecutable(tokens) {
				tokens = append(tokens, ";")
				tokens = append(tokens, executableSQLTokens(content[bodyStart:bodyStart+bodyEnd])...)
				tokens = append(tokens, ";")
			}
			index = bodyStart + bodyEnd
			if index+len(tag) <= len(content) {
				index += len(tag)
			}
		case isWordByte(content[index]):
			start := index
			for index < len(content) && isWordByte(content[index]) {
				index++
			}
			tokens = append(tokens, strings.ToLower(content[start:index]))
		case strings.ContainsRune(".,;()", rune(content[index])):
			tokens = append(tokens, string(content[index]))
			index++
		default:
			index++
		}
	}
	return tokens
}

func skipSingleQuoted(content string, index int) int {
	index++
	for index < len(content) {
		if content[index] != '\'' {
			index++
			continue
		}
		if index+1 < len(content) && content[index+1] == '\'' {
			index += 2
			continue
		}
		return index + 1
	}
	return index
}

func readQuotedIdentifier(content string, index int) (string, int) {
	index++
	var value strings.Builder
	for index < len(content) {
		if content[index] != '"' {
			value.WriteByte(content[index])
			index++
			continue
		}
		if index+1 < len(content) && content[index+1] == '"' {
			value.WriteByte('"')
			index += 2
			continue
		}
		return value.String(), index + 1
	}
	return value.String(), index
}

func dollarQuoteTag(content string) (string, bool) {
	if len(content) < 2 || content[0] != '$' {
		return "", false
	}
	end := strings.IndexByte(content[1:], '$')
	if end < 0 {
		return "", false
	}
	end++
	for _, character := range content[1:end] {
		if character != '_' && !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			return "", false
		}
	}
	return content[:end+1], true
}

func dollarBodyExecutable(tokens []string) bool {
	for index := len(tokens) - 1; index >= 0 && index >= len(tokens)-8; index-- {
		switch tokens[index] {
		case "as", "do", "execute":
			return true
		case ";":
			return false
		}
	}
	return false
}

func isWordByte(value byte) bool {
	return value == '_' || value == '$' ||
		(value >= 'a' && value <= 'z') ||
		(value >= 'A' && value <= 'Z') ||
		(value >= '0' && value <= '9')
}
