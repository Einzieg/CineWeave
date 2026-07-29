package provider

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRuntimePaidGatewayRequestsHaveDurableOrDirectBillingIdentity(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	paidTypes := map[string]bool{
		"GatewayTextRequest":            true,
		"GatewayImageRequest":           true,
		"GatewayTTSRequest":             true,
		"GatewayASRRequest":             true,
		"GatewayVideoCreateTaskRequest": true,
	}
	for _, relativeDir := range []string{"internal/api", "internal/workflows"} {
		dir := filepath.Join(root, relativeDir)
		entries, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range entries {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			fileSet := token.NewFileSet()
			file, err := parser.ParseFile(fileSet, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				selector, ok := literal.Type.(*ast.SelectorExpr)
				if !ok || !paidTypes[selector.Sel.Name] {
					return true
				}
				fields := map[string]bool{}
				for _, element := range literal.Elts {
					keyed, ok := element.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					identifier, ok := keyed.Key.(*ast.Ident)
					if ok {
						fields[identifier.Name] = true
					}
				}
				if !fields["ProjectID"] {
					return true
				}
				if fields["WorkflowRunID"] || fields["GatewayBillingIdentity"] {
					return true
				}
				position := fileSet.Position(literal.Pos())
				t.Errorf(
					"%s:%d: %s with projectId must carry workflowRunId or explicit GatewayBillingIdentity",
					filepath.ToSlash(position.Filename),
					position.Line,
					selector.Sel.Name,
				)
				return true
			})
		}
	}
}
