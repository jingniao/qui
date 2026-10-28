package gate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Source-bearing implementations have no logger, audit or database dependency.
// Keep this test scoped to the new privacy-sensitive files, not the repo root.
func TestAddressImplementationsHaveNoPersistenceOrLoggingCalls(t *testing.T) {
	files := []string{"../core/xray/device_gate.go", "../core/singbox/device_gate.go", "../collector/singbox_sources.go", "../observer/store.go", "../../panel/connections/admission.go", "../../panel/user/device_observer.go"}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, data, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			if strings.Contains(imp.Path.Value, "log/slog") || strings.Contains(imp.Path.Value, "database/sql") {
				t.Fatalf("private implementation gained persistence/logging dependency: %s", name)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "Debug", "Info", "Warn", "Error", "Log", "ExecContext", "QueryContext", "WriteFile":
				t.Errorf("review new persistence/logging call in %s", name)
			}
			return true
		})
	}
}
