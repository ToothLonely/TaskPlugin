package diagnostic

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

func TestCatalogFormats(t *testing.T) {
	verbs := regexp.MustCompile(`%[-+# 0]*(?:\d+)?(?:\.\d+)?[a-zA-Z%]`)
	for source, translated := range english {
		if translated == "" || strings.ContainsFunc(translated, func(r rune) bool { return unicode.In(r, unicode.Cyrillic) }) {
			t.Errorf("missing English translation for %q", source)
		}
		if !reflect.DeepEqual(verbs.FindAllString(source, -1), verbs.FindAllString(translated, -1)) {
			t.Errorf("format arguments differ: %q -> %q", source, translated)
		}
	}
}

func TestCatalogCoversDiagnosticSources(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || filepath.Base(filepath.Dir(path)) == "diagnostic" {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			check := false
			switch function := call.Fun.(type) {
			case *ast.Ident:
				check = function.Name == "invalid" && file.Name.Name == "task"
			case *ast.SelectorExpr:
				if pkg, ok := function.X.(*ast.Ident); ok {
					check = pkg.Name == "diagnostic" && (function.Sel.Name == "New" || function.Sel.Name == "Errorf")
				}
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !check || !ok || literal.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.ContainsFunc(text, func(r rune) bool { return unicode.In(r, unicode.Cyrillic) }) {
				if _, ok := english[text]; !ok {
					t.Errorf("%s: missing translation for %q", fset.Position(literal.Pos()), text)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
