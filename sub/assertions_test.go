package sub

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A bare type assertion on a decoded JSON value panics whenever the field is
// absent or of another type, and Xray configurations legitimately omit most of
// them. Because gin recovers the panic, the symptom is a 500 on the whole
// subscription rather than a crash, which is easy to miss and hard to trace.
//
// The helpers in jsonvalue.go read the same values safely, so this test insists
// the package keeps using them: assert with the two-value form, or go through a
// helper.
func TestNoUncheckedTypeAssertions(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}

	var findings []string
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			// An assertion whose result is assigned to two variables already
			// handles the failure case.
			checked := map[ast.Node]bool{}
			ast.Inspect(file, func(n ast.Node) bool {
				assign, ok := n.(*ast.AssignStmt)
				if ok && len(assign.Lhs) == 2 && len(assign.Rhs) == 1 {
					if assertion, ok := assign.Rhs[0].(*ast.TypeAssertExpr); ok {
						checked[assertion] = true
					}
				}
				return true
			})

			ast.Inspect(file, func(n ast.Node) bool {
				assertion, ok := n.(*ast.TypeAssertExpr)
				// A nil Type means `x.(type)` inside a type switch, which is safe.
				if !ok || assertion.Type == nil || checked[assertion] {
					return true
				}
				position := fset.Position(assertion.Pos())
				findings = append(findings,
					strings.TrimPrefix(path, "./")+":"+strconv.Itoa(position.Line))
				return true
			})
		}
	}

	sort.Strings(findings)
	if len(findings) > 0 {
		t.Errorf("%d unchecked type assertion(s); use the two-value form or a helper from jsonvalue.go:\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}
