package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// TestEveryServiceFieldIsWired reads services.go and asserts that every field
// of the Services struct is assigned a value in the `return Services{...}`
// literal at the end of Build.
//
// This is a merge guard, not a design check. Build is the composition root: a
// field it forgets stays nil, nothing fails to compile, and the first request
// that reaches the handler behind it panics in production. That has happened
// twice on this fork — a union merge of two branches that both touched the
// literal dropped Chats, Projects, Schedules and Runs, and the crash only
// surfaced when someone opened the projects list.
//
// Constructing Services for real needs a container runtime and a data
// directory, so this reads the source instead. It is deliberately dumb: it
// cannot tell a correct value from a wrong one, only a present key from an
// absent one, which is exactly the failure mode a merge produces.
func TestEveryServiceFieldIsWired(t *testing.T) {
	assertLiteralCoversStruct(t, "services.go", "Services")
}

func assertLiteralCoversStruct(t *testing.T, file, typeName string) {
	t.Helper()

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, filepath.Clean(file), nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}

	declared := declaredFields(parsed, typeName)
	if len(declared) == 0 {
		t.Fatalf("no fields found on %s in %s — has the struct been renamed?", typeName, file)
	}
	assigned := assignedKeys(parsed, typeName)
	if len(assigned) == 0 {
		t.Fatalf("no `%s{...}` composite literal found in %s", typeName, file)
	}

	for _, field := range declared {
		if !assigned[field] {
			t.Errorf(
				"%s.%s is never assigned in the %s{...} literal: it will be nil at run time, "+
					"and the handler behind it will panic on the first request. "+
					"If a merge dropped the line, put it back; if the field is genuinely "+
					"unused, delete it from the struct.",
				typeName, field, typeName,
			)
		}
	}
}

// declaredFields lists the exported field names of one struct type.
func declaredFields(file *ast.File, typeName string) []string {
	var fields []string
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Name.Name != typeName {
			return true
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, field := range structType.Fields.List {
			for _, name := range field.Names {
				if name.IsExported() {
					fields = append(fields, name.Name)
				}
			}
		}
		return false
	})
	return fields
}

// assignedKeys collects the keys of every `typeName{...}` composite literal in
// the file. There is one such literal today; taking the union of all of them
// keeps the test honest if Build ever grows an early return.
func assignedKeys(file *ast.File, typeName string) map[string]bool {
	keys := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		ident, ok := lit.Type.(*ast.Ident)
		if !ok || ident.Name != typeName {
			return true
		}
		for _, element := range lit.Elts {
			kv, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if key, ok := kv.Key.(*ast.Ident); ok {
				keys[key.Name] = true
			}
		}
		return true
	})
	return keys
}
