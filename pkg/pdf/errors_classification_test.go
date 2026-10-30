package pdf

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// otherErrors lists the sentinels that do not come from the caller's input:
// engine failures, misuse of the API, a remote service refusing a request,
// and outcomes such as [ErrNoMetadata] that callers handle themselves.
var otherErrors = []error{
	ErrInternalPanic,
	ErrOCRFailed,
	ErrNilSource,
	ErrWriterConsumed,
	ErrWriterClosed,
	ErrTimestampAuthority,
	ErrValidationData,
	ErrSchematronRuleSkipped,
	ErrNoMetadata,
	ErrPipelineEmpty,
	ErrPipelineNoSource,
}

// TestEverySentinelIsClassified fails when an exported sentinel joins
// errors.go without a place in invalidInputErrors or otherErrors, so that a
// new sentinel never reaches a caller of [IsInvalidInput] unclassified.
func TestEverySentinelIsClassified(t *testing.T) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "errors.go", nil, 0)
	if err != nil {
		t.Fatalf("parse errors.go: %v", err)
	}

	self, err := parser.ParseFile(fset, "errors_classification_test.go", nil, 0)
	if err != nil {
		t.Fatalf("parse errors_classification_test.go: %v", err)
	}
	file.Decls = append(file.Decls, self.Decls...)

	var sentinels []string
	lists := make(map[string][]string)

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}

		for _, spec := range gen.Specs {
			value := spec.(*ast.ValueSpec)
			for i, name := range value.Names {
				switch {
				case name.Name == "invalidInputErrors" || name.Name == "otherErrors":
					for _, elt := range value.Values[i].(*ast.CompositeLit).Elts {
						lists[name.Name] = append(lists[name.Name], elt.(*ast.Ident).Name)
					}
				case strings.HasPrefix(name.Name, "Err"):
					sentinels = append(sentinels, name.Name)
				}
			}
		}
	}

	if len(sentinels) == 0 {
		t.Fatal("found no sentinel in errors.go")
	}

	seen := make(map[string]int)
	for _, list := range lists {
		for _, name := range list {
			seen[name]++
		}
	}

	for _, sentinel := range sentinels {
		switch seen[sentinel] {
		case 1:
		case 0:
			t.Errorf("%s is neither in invalidInputErrors nor in otherErrors", sentinel)
		default:
			t.Errorf("%s is listed %d times", sentinel, seen[sentinel])
		}
		delete(seen, sentinel)
	}

	for name := range seen {
		t.Errorf("%s is listed but is not a sentinel of errors.go", name)
	}
}

func TestIsInvalidInput(t *testing.T) {
	for _, err := range invalidInputErrors {
		wrapped := fmt.Errorf("stage: %w", err)
		if !IsInvalidInput(wrapped) {
			t.Errorf("IsInvalidInput(%v) = false, want true", wrapped)
		}
	}

	for _, err := range otherErrors {
		wrapped := fmt.Errorf("stage: %w", err)
		if IsInvalidInput(wrapped) {
			t.Errorf("IsInvalidInput(%v) = true, want false", wrapped)
		}
	}

	if IsInvalidInput(errors.New("boom")) {
		t.Error("IsInvalidInput(boom) = true, want false")
	}
}
