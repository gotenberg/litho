package flatten

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// compositeImage returns the dictionary and decoded samples of the first
// composite image the flattened data's page draws.
func compositeImage(t *testing.T, data []byte) (ast.PdfDict, []byte) {
	t.Helper()
	ctx := context.Background()
	doc, pg := open(t, data)
	resources, err := document.ResolveDict(ctx, doc, pg.Dict["Resources"])
	if err != nil {
		t.Fatal(err)
	}
	xobjects, err := document.ResolveDict(ctx, doc, resources["XObject"])
	if err != nil {
		t.Fatal(err)
	}
	for name, obj := range xobjects {
		if !strings.HasPrefix(string(name), "FlatIm") {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, obj)
		if err != nil {
			t.Fatal(err)
		}
		rc, err := doc.NewStreamReader(ctx, stream)
		if err != nil {
			t.Fatal(err)
		}
		samples, err := io.ReadAll(rc)
		if closeErr := rc.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			t.Fatal(err)
		}
		return stream.Dict, samples
	}
	t.Fatal("no composite image")
	return nil, nil
}

// TestFlattenCMYKPageKeepsCMYK checks that a page whose group blends in
// DeviceCMYK is composited in it (ISO 32000-2, 11.4.7): the composite is a
// DeviceCMYK image, and a half-transparent cyan over black ink is cyan and
// black ink, with no ink a trip through RGB would add.
func TestFlattenCMYKPageKeepsCMYK(t *testing.T) {
	t.Parallel()
	out, _ := checkFlattened(t, fixture{
		page:    "/Group << /S /Transparency /CS /DeviceCMYK >>",
		content: "0 0 0 1 k 0 0 100 100 re f q /GS1 gs 1 0 0 0 k 10 10 30 30 re f Q",
	})
	dict, samples := compositeImage(t, out)
	if cs, _ := dict["ColorSpace"].(ast.PdfName); cs != "DeviceCMYK" {
		t.Fatalf("composite colour space %v, want DeviceCMYK", dict["ColorSpace"])
	}
	w, _ := dict["Width"].(ast.PdfInt)
	h, _ := dict["Height"].(ast.PdfInt)
	if len(samples) != 4*int(w)*int(h) {
		t.Fatalf("%d samples for a %dx%d CMYK image", len(samples), w, h)
	}
	// The image's centre lies inside the square: C 0.5, K 0.5.
	i := 4 * (int(h)/2*int(w) + int(w)/2)
	got := samples[i : i+4]
	if got[0] < 126 || got[0] > 129 || got[1] != 0 || got[2] != 0 || got[3] < 126 || got[3] > 129 {
		t.Errorf("composite centre = %v, want C 128 K 128 alone", got)
	}
}
