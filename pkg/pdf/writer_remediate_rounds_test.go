package pdf

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/remediation"
)

// errMissingCatalogKey is the residual the test engine of
// [roundsEngine] validates to while the catalog lacks a key it sets.
var errMissingCatalogKey = errors.New("catalog lacks a key the engine sets")

// roundsStep is what [roundsEngine] does in one round: the catalog keys it
// sets, each a structural repair, and whether it then empties every page's
// content, a content-tier repair that drops the text a rollback restores.
type roundsStep struct {
	keys  []string
	empty bool
}

// roundsEngine is a remediation engine that makes the repairs of steps, one
// step per round, and records the fixers each round was told to disable.
type roundsEngine struct {
	steps    []roundsStep
	disabled [][]string
}

// run is the [remediationRun] of the engine.
func (e *roundsEngine) run(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error) {
	e.disabled = append(e.disabled, slices.Clone(disabled))
	round := len(e.disabled) - 1
	if round >= len(e.steps) {
		return &remediation.Report{}, nil
	}
	step := e.steps[round]
	rep := &remediation.Report{}

	root, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, errors.New("no catalog reference")
	}
	obj, err := doc.Resolve(ctx, root)
	if err != nil {
		return nil, err
	}
	catalog, ok := obj.(ast.PdfDict)
	if !ok {
		return nil, errors.New("no catalog")
	}
	catalog = maps.Clone(catalog)
	for _, k := range step.keys {
		catalog[ast.PdfName(k)] = ast.PdfName("Set")
		rep.Fixed = append(rep.Fixed, "set /"+k)
	}
	alloc.AllocAt(root.ObjectNum, catalog)

	name := "empty-content-" + string(rune('a'+round))
	if step.empty && !slices.Contains(disabled, name) {
		snap := alloc.Snapshot()
		rep.ContentSnapshot = &snap
		cat, err := document.ResolveCatalog(ctx, doc)
		if err != nil {
			return nil, err
		}
		pages, err := document.ResolvePages(ctx, doc, cat)
		if err != nil {
			return nil, err
		}
		for _, p := range pages {
			if ref, ok := p.Dict["Contents"].(ast.PdfIndirectRef); ok {
				alloc.AllocAt(ref.ObjectNum, ast.NewStream(ast.PdfDict{}, nil))
			}
		}
		desc := "emptied the page content in round " + string(rune('1'+round))
		rep.Fixed = append(rep.Fixed, desc)
		rep.ContentFixed = []string{desc}
		rep.ContentFixers = []string{name}
	}
	return rep, nil
}

// revalidate reports [errMissingCatalogKey] while the catalog of r lacks one
// of the keys the steps set.
func (e *roundsEngine) revalidate(ctx context.Context, r *Reader) ([]error, error) {
	root, _ := r.doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	catalog, err := document.ResolveDict(ctx, r.doc, root)
	if err != nil {
		return nil, err
	}
	for _, s := range e.steps {
		for _, k := range s.keys {
			if _, ok := catalog[ast.PdfName(k)]; !ok {
				return []error{errMissingCatalogKey}, nil
			}
		}
	}
	return nil, nil
}

// TestWriterRemediateContinuesAfterRollback pins that the remediation rounds
// go on after a rollback of the content tier, with the fixers it rolled back
// off and the others on, in the first round and in a later one alike. A
// rollback used to end the remediation, and the repairs the later rounds make
// were lost.
func TestWriterRemediateContinuesAfterRollback(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		steps        []roundsStep
		wantDisabled [][]string
		wantFixed    []string
		wantRolled   []string
	}{
		{
			name:         "a rollback in the first round",
			steps:        []roundsStep{{keys: []string{"PageLayout"}, empty: true}, {keys: []string{"PageMode"}}},
			wantDisabled: [][]string{nil, {"empty-content-a"}},
			wantFixed:    []string{"set /PageLayout", "set /PageMode"},
			wantRolled:   []string{"emptied the page content in round 1"},
		},
		{
			name: "a rollback in the second round",
			steps: []roundsStep{
				{keys: []string{"PageLayout"}},
				{keys: []string{"PageMode"}, empty: true},
				{keys: []string{"Lang"}},
			},
			wantDisabled: [][]string{nil, nil, {"empty-content-b"}},
			wantFixed:    []string{"set /PageLayout", "set /PageMode", "set /Lang"},
			wantRolled:   []string{"emptied the page content in round 2"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := threePagePDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()

			e := &roundsEngine{steps: tc.steps}
			w := NewWriter(r)
			report, err := w.remediate(ctx, DefaultRegressionTolerance, e.revalidate, e.run)
			if err != nil {
				t.Fatalf("remediate: %v", err)
			}
			if !slices.EqualFunc(e.disabled, tc.wantDisabled, slices.Equal) {
				t.Errorf("the rounds disabled %q, want %q", e.disabled, tc.wantDisabled)
			}
			if !slices.Equal(report.Fixed, tc.wantFixed) {
				t.Errorf("Fixed = %q, want %q", report.Fixed, tc.wantFixed)
			}
			if !slices.Equal(report.RolledBack, tc.wantRolled) {
				t.Errorf("RolledBack = %q, want %q", report.RolledBack, tc.wantRolled)
			}
			if len(report.Residual) != 0 {
				t.Errorf("Residual = %v, want none", report.Residual)
			}

			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
			if err != nil {
				t.Fatalf("reopening the output: %v", err)
			}
			defer r2.Close()
			if res, err := e.revalidate(ctx, r2); err != nil || len(res) != 0 {
				t.Errorf("the output misses a repair: %v %v", res, err)
			}
			text, err := r2.ExtractAllText(ctx)
			if err != nil {
				t.Fatalf("ExtractAllText: %v", err)
			}
			for _, want := range []string{"Page1", "Page2", "Page3"} {
				if !strings.Contains(text, want) {
					t.Errorf("the output lost %q: %q", want, text)
				}
			}
		})
	}
}
