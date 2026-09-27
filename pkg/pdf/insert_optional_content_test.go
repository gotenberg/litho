package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// optionalContentPagePDF builds a one-page document drawing two optional
// content groups, Visible<suffix> and Hidden<suffix>, whose default
// configuration is config. A config of "" leaves the catalog without
// /OCProperties.
func optionalContentPagePDF(suffix, config string) []byte {
	const content = "/OC /MCvis BDC BT ET EMC /OC /MChid BDC BT ET EMC\n"
	catalog := "<< /Type /Catalog /Pages 2 0 R >>"
	if config != "" {
		catalog = "<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [5 0 R 6 0 R] /D " + config + " >> >>"
	}
	return buildPDF([]string{
		catalog,
		"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Properties << /MCvis 5 0 R /MChid 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
		"<< /Type /OCG /Name (Visible" + suffix + ") >>",
		"<< /Type /OCG /Name (Hidden" + suffix + ") >>",
	})
}

// optionalContentState is what an output's optional content says of the groups
// its pages draw.
type optionalContentState struct {
	name     string
	unlisted []string
	shown    []string
	hidden   []string
	ordered  bool
}

// readOptionalContentState reads the /OCProperties of data against the groups
// its pages name in /Properties, by group /Name.
func readOptionalContentState(t *testing.T, data []byte) optionalContentState {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	ocp, _ := document.ResolveDict(ctx, doc, catalog.Dict["OCProperties"])
	config, _ := document.ResolveDict(ctx, doc, ocp["D"])
	refs := func(obj ast.PdfObject) map[int]bool {
		out := map[int]bool{}
		arr, _ := document.ResolveArray(ctx, doc, obj)
		for _, e := range arr {
			if ref, isRef := e.(ast.PdfIndirectRef); isRef {
				out[ref.ObjectNum] = true
			}
		}
		return out
	}
	listed, on, off, order := refs(ocp["OCGs"]), refs(config["ON"]), refs(config["OFF"]), refs(config["Order"])
	baseOff := config["BaseState"] == ast.PdfName("OFF")
	var state optionalContentState
	if name, err := document.ResolveString(ctx, doc, config["Name"]); err == nil {
		state.name = document.DecodePdfString(name)
	}
	state.ordered = true
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	for _, page := range pages {
		res, _ := document.ResolveDict(ctx, doc, page.Dict["Resources"])
		props, _ := document.ResolveDict(ctx, doc, res["Properties"])
		for _, key := range []ast.PdfName{"MCvis", "MChid"} {
			ref, _ := props[key].(ast.PdfIndirectRef)
			group, _ := document.ResolveDict(ctx, doc, ref)
			name, _ := document.ResolveString(ctx, doc, group["Name"])
			label := document.DecodePdfString(name)
			if !listed[ref.ObjectNum] {
				state.unlisted = append(state.unlisted, label)
				continue
			}
			if !order[ref.ObjectNum] {
				state.ordered = false
			}
			if off[ref.ObjectNum] || (baseOff && !on[ref.ObjectNum]) {
				state.hidden = append(state.hidden, label)
			} else {
				state.shown = append(state.shown, label)
			}
		}
	}
	slices.Sort(state.shown)
	slices.Sort(state.hidden)
	return state
}

// TestInsertPages_CarriesTheOptionalContentOfTheInsertedPages pins that pages
// inserted into a document keep drawing their optional content as their source
// configured it: ISO 32000-1, 8.11.4.3 asks /OCProperties /OCGs to list every
// optional content group of the document, and 8.11.2.3 draws a group's content
// as the default configuration says. The inserted groups joined neither, so
// the source's hidden layer was drawn. A target with no optional content gets
// a configuration of its own, named as ISO 14289-1, 7.10 and ISO 19005-2, 6.9
// ask, and every group joins /Order, which ISO 19005-2, 6.9 asks to hold them
// all.
func TestInsertPages_CarriesTheOptionalContentOfTheInsertedPages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const hiding = "<< /Name (Layers) /Order [5 0 R 6 0 R] /OFF [6 0 R] >>"
	for _, tc := range []struct {
		name   string
		target string
		source string
		want   optionalContentState
	}{
		{
			"both configured", hiding, hiding,
			optionalContentState{name: "Layers", shown: []string{"VisibleS", "VisibleT"}, hidden: []string{"HiddenS", "HiddenT"}, ordered: true},
		},
		{
			"target shows by list", "<< /Name (Base) /BaseState /OFF /ON [5 0 R] /Order [5 0 R 6 0 R] >>", hiding,
			optionalContentState{name: "Base", shown: []string{"VisibleS", "VisibleT"}, hidden: []string{"HiddenS", "HiddenT"}, ordered: true},
		},
		{
			"target without optional content", "", hiding,
			// The target's own groups were listed nowhere, and stay so.
			optionalContentState{name: "Layers", shown: []string{"VisibleS"}, hidden: []string{"HiddenS"}, unlisted: []string{"HiddenT", "VisibleT"}, ordered: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			target := openDeclaration(t, optionalContentPagePDF("T", tc.target), false)
			defer target.Close()
			source := openDeclaration(t, optionalContentPagePDF("S", tc.source), false)
			defer source.Close()
			w := NewWriter(target)
			defer w.Close()
			if err := w.InsertPages(1, source); err != nil {
				t.Fatalf("InsertPages: %v", err)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			got := readOptionalContentState(t, out.Bytes())
			slices.Sort(got.unlisted)
			if got.name != tc.want.name || !slices.Equal(got.shown, tc.want.shown) || !slices.Equal(got.hidden, tc.want.hidden) ||
				!slices.Equal(got.unlisted, tc.want.unlisted) || got.ordered != tc.want.ordered {
				t.Errorf("optional content = %+v, want %+v", got, tc.want)
			}
		})
	}
}
