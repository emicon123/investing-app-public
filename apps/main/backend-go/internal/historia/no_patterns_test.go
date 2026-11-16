package historia

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoBehavioralQuartetTypesAST pins doc 13 §5 smell #3 as a negative acceptance
// criterion: the Go historia package must contain no Iterator/Memento/Caretaker/
// Visitor types. A Go slice already iterates, a struct copied by value already is a
// memento, and there is no undo requirement — porting the quartet would be ceremony
// for a Java-specific problem. Verified by graph query before implementation: the
// only callers are HistoriaService.listHistoria and XirrService.computeXirr.
func TestNoBehavioralQuartetTypesAST(t *testing.T) {
	forbidden := []string{"Iterator", "Caretaker", "Memento", "Visitor"}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	parsed := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		parsed++
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				for _, sub := range forbidden {
					if strings.Contains(ts.Name.Name, sub) {
						t.Fatalf("forbidden behavioral-quartet type %q in %s (doc 13 §5 smell #3)",
							ts.Name.Name, file)
					}
				}
			}
		}
	}
	if parsed == 0 {
		t.Fatal("no non-test source files found — the scan would pass vacuously")
	}
}
