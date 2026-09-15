// Command i18n-keys lists the backend text the web interface shows, under the
// keys the frontend's translation shim looks up (frontend/src/i18n/normalize.ts).
//
//	go run ./cmd/i18n-keys > ../frontend/src/i18n/catalog/en.server-keys.json
//	go run ./cmd/i18n-keys -refs    # the same keys with their source locations
//
// The Arabic interface translates server text — error messages, dashboard
// alerts, agent sign-in instructions, health reasons — on the client, keyed by
// the English. This command finds that text in the Go source so the catalog
// check can report what an upstream sync added, the same way the frontend
// extractor does for components. It is a heuristic reader, not a type checker:
//
//   - a string that reads as prose (capitalised or ending in sentence
//     punctuation, two words or a capitalised word) anywhere outside tests,
//     log calls and struct tags;
//   - any string of two words or more passed to errors.New, fmt.Errorf or
//     SendErr in the transport, service and config packages, since
//     user-facing errors are lowercase by Go convention (errors deeper down
//     are plumbing, and reach people only through those layers);
//   - fmt.Sprintf / fmt.Errorf formats, with %d as {n} and other verbs as {s},
//     and `"text " + value` concatenations, with the value as {s}.
//
// Left out: errors that wrap another (%w, a "doing x:" prefix), SQL, and
// multi-paragraph text, which is instructions for an agent, not interface.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	number      = regexp.MustCompile(`\d+(?:[.,]\d+)*`)
	space       = regexp.MustCompile(`\s+`)
	twoLetters  = regexp.MustCompile(`[A-Za-z]{2}`)
	verb        = regexp.MustCompile(`%[-+# 0]*(?:\d+|\*)?(?:\.(?:\d+|\*))?([a-zA-Z%])`)
	wrapOnly    = regexp.MustCompile(`%w`)
	codeShaped  = regexp.MustCompile(`^[a-z0-9_.\-/]+$|^[A-Z0-9_]+$|://|^\W|\.(go|json|yaml|toml|sh|md)$`)
	slotOnly    = regexp.MustCompile(`^[\s{}ns:·,.\-—()]*$`)
	sentenceEnd = regexp.MustCompile(`[.?!…:]$`)
	upperStart  = regexp.MustCompile(`^[A-Z]`)
)

// keyOf mirrors keyOf in frontend/src/i18n/normalize.ts.
func keyOf(text string) (string, bool) {
	core := strings.TrimSpace(text)
	if !twoLetters.MatchString(core) {
		return "", false
	}
	core = space.ReplaceAllString(core, " ")
	return number.ReplaceAllString(core, "{n}"), true
}

func readsAsProse(text string) bool {
	core := strings.TrimSpace(text)
	if !twoLetters.MatchString(core) {
		return false
	}
	words := 0
	for _, word := range strings.Fields(core) {
		if twoLetters.MatchString(word) {
			words++
		}
	}
	if words < 2 && !regexp.MustCompile(`^[A-Z][a-z]+$`).MatchString(core) {
		return false
	}
	return upperStart.MatchString(core) || sentenceEnd.MatchString(core)
}

// fromFormat turns a Printf format into key text: %d → {n}, other verbs → {s}.
func fromFormat(format string) string {
	return verb.ReplaceAllStringFunc(format, func(match string) string {
		switch match[len(match)-1] {
		case '%':
			return "%"
		case 'd':
			return "{n}"
		default:
			return "{s}"
		}
	})
}

type extractor struct {
	fset *token.FileSet
	keys map[string][]string
	// userFacing is true for packages whose errors are shown to people.
	userFacing bool
}

var (
	sql           = regexp.MustCompile(`^(SELECT|INSERT|UPDATE|DELETE|CREATE|DROP|ALTER|PRAGMA|WITH)\b`)
	plumbing      = regexp.MustCompile(`:$|; output: |^\{s\}:`)
	userFacingDir = regexp.MustCompile(`^internal/(transport|service|config)/`)
)

func (e *extractor) add(text string, pos token.Pos) {
	if strings.Count(text, "\n") > 2 || len(text) > 600 || sql.MatchString(strings.TrimSpace(text)) {
		return
	}
	key, ok := keyOf(text)
	if ok && plumbing.MatchString(key) {
		return
	}
	if !ok || strings.Contains(key, "{s}{s}") {
		return
	}
	literal := strings.NewReplacer("{n}", " ", "{s}", " ").Replace(key)
	if !twoLetters.MatchString(literal) || slotOnly.MatchString(key) || codeShaped.MatchString(strings.TrimSpace(literal)) {
		return
	}
	at := e.fset.Position(pos)
	e.keys[key] = append(e.keys[key], fmt.Sprintf("%s:%d", filepath.ToSlash(at.Filename), at.Line))
}

func stringValue(expr ast.Expr) (string, bool) {
	literal, ok := expr.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

// composed returns the text of `"a " + b + " c"` with non-literal operands as {s}.
func composed(expr ast.Expr) (string, bool) {
	switch node := expr.(type) {
	case *ast.BasicLit:
		return stringValue(node)
	case *ast.ParenExpr:
		return composed(node.X)
	case *ast.BinaryExpr:
		if node.Op != token.ADD {
			return "", false
		}
		left, leftOK := composed(node.X)
		right, rightOK := composed(node.Y)
		if !leftOK && !rightOK {
			return "", false
		}
		if !leftOK {
			left = "{s}"
		}
		if !rightOK {
			right = "{s}"
		}
		return left + right, true
	default:
		return "", false
	}
}

func calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		if pkg, ok := fn.X.(*ast.Ident); ok {
			return pkg.Name + "." + fn.Sel.Name
		}
		return fn.Sel.Name
	case *ast.Ident:
		return fn.Name
	}
	return ""
}

var (
	logCalls   = regexp.MustCompile(`^(log|slog|logger|l)\.|^fmt\.(Print|Fprint)|\.(Debug|Info|Warn|Error)f?$|^t\.|^b\.`)
	formatters = map[string]bool{"fmt.Sprintf": true, "fmt.Errorf": true}
)

// isErrorCall reports calls whose string argument becomes an error message a
// person may read: errors.New, fmt.Errorf, http.Error, and SendErr from any
// package that exposes it.
func isErrorCall(name string) bool {
	return name == "errors.New" || name == "fmt.Errorf" || name == "http.Error" || name == "SendErr" || strings.HasSuffix(name, ".SendErr")
}

func (e *extractor) file(path string) error {
	parsed, err := parser.ParseFile(e.fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	handled := map[ast.Node]bool{}
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.ImportSpec, *ast.Field:
			return false // import paths, struct tags
		case *ast.CallExpr:
			name := calleeName(n)
			if logCalls.MatchString(name) && !isErrorCall(name) {
				return false
			}
			for index, arg := range n.Args {
				if formatters[name] && index == 0 {
					if format, ok := stringValue(arg); ok {
						handled[arg] = true
						if wrapOnly.MatchString(format) {
							continue
						}
						text := fromFormat(format)
						if readsAsProse(text) || (isErrorCall(name) && e.userFacing && len(strings.Fields(text)) >= 2) {
							e.add(text, arg.Pos())
						}
					}
					continue
				}
				if isErrorCall(name) {
					if text, ok := composed(arg); ok {
						markLiterals(arg, handled)
						if (e.userFacing && len(strings.Fields(text)) >= 2) || readsAsProse(text) {
							e.add(text, arg.Pos())
						}
					}
				}
			}
		case *ast.BinaryExpr:
			if n.Op == token.ADD && !handled[n] {
				if text, ok := composed(n); ok && strings.Contains(text, "{s}") {
					markLiterals(n, handled)
					if readsAsProse(strings.ReplaceAll(text, "{s}", "x")) {
						e.add(text, n.Pos())
					}
				}
			}
		case *ast.BasicLit:
			if n.Kind == token.STRING && !handled[n] {
				if text, ok := stringValue(n); ok && readsAsProse(text) && !strings.Contains(text, "\n\n") {
					e.add(text, n.Pos())
				}
			}
		}
		return true
	})
	return nil
}

func markLiterals(node ast.Node, handled map[ast.Node]bool) {
	ast.Inspect(node, func(child ast.Node) bool {
		if child != nil {
			handled[child] = true
		}
		return true
	})
}

func main() {
	withRefs := flag.Bool("refs", false, "print {key, refs} objects with source locations instead of plain keys")
	flag.Parse()
	root := "internal"
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}
	e := &extractor{fset: token.NewFileSet(), keys: map[string][]string{}}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		e.userFacing = userFacingDir.MatchString(filepath.ToSlash(path))
		return e.file(path)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	keys := make([]string, 0, len(e.keys))
	for key := range e.keys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var output any = keys
	if *withRefs {
		type entry struct {
			Key  string   `json:"key"`
			Refs []string `json:"refs"`
		}
		list := make([]entry, 0, len(keys))
		for _, key := range keys {
			list = append(list, entry{Key: key, Refs: e.keys[key]})
		}
		output = list
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
