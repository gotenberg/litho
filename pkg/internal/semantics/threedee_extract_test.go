package semantics

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// model3DPDF builds a one-page document whose single annotation and 3D stream
// are supplied by the caller, so each test varies one construct.
func model3DPDF(annot, stream string) []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] >>",
		annot,
		stream,
	})
}

// streamObj renders a stream object whose /Length matches its payload, so the
// reader takes the ordinary path rather than recovering a bad length.
func streamObj(dictExtras, payload string) string {
	return fmt.Sprintf("<< /Length %d %s >>\nstream\n%s\nendstream", len(payload), dictExtras, payload)
}

// extractModels runs [Extract3DModels] over page 1 of the fixture.
func extractModels(t *testing.T, data []byte) []models.Model3D {
	t.Helper()

	ctx := context.Background()
	doc := openBytes(t, data)
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("fixture has no pages")
	}
	got, err := Extract3DModels(ctx, doc, pages[0])
	if err != nil {
		t.Fatalf("Extract3DModels: %v", err)
	}
	return got
}

// TestExtract3DModelsWithoutAnnots pins that a page carrying no /Annots yields
// nothing rather than an error. Most pages have no annotations at all, so this
// is the common path.
func TestExtract3DModelsWithoutAnnots(t *testing.T) {
	t.Parallel()

	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
	})
	if got := extractModels(t, data); len(got) != 0 {
		t.Errorf("got %d model(s) from a page with no /Annots, want 0", len(got))
	}
}

// TestExtract3DModelsIgnoresOtherSubtypes pins that an annotation of another
// subtype is skipped even when it carries a /3DD entry. Only a /3D annotation
// holds 3D artwork.
func TestExtract3DModelsIgnoresOtherSubtypes(t *testing.T) {
	t.Parallel()

	data := model3DPDF(
		"<< /Type /Annot /Subtype /Widget /Rect [0 0 20 20] /3DD 5 0 R >>",
		streamObj("/Subtype /U3D", "U3D payload"),
	)
	if got := extractModels(t, data); len(got) != 0 {
		t.Errorf("got %d model(s) from a /Widget annotation, want 0", len(got))
	}
}

// TestExtract3DModelsSkipsAnnotationWithout3DD pins that a /3D annotation with
// no /3DD entry is skipped. The 3D stream holds the artwork, so an annotation
// without one has nothing to extract.
func TestExtract3DModelsSkipsAnnotationWithout3DD(t *testing.T) {
	t.Parallel()

	data := model3DPDF(
		"<< /Type /Annot /Subtype /3D /Rect [0 0 20 20] >>",
		streamObj("/Subtype /U3D", "U3D payload"),
	)
	if got := extractModels(t, data); len(got) != 0 {
		t.Errorf("got %d model(s) from a /3D annotation with no /3DD, want 0", len(got))
	}
}

// TestExtract3DModelsReadsStreamSubtype pins both values the 3D stream
// dictionary's /Subtype may take. Per ISO 32000-2:2020, 13.6.3 that entry is
// U3D or PRC, and it is the primary discriminator: the magic-byte sniff below
// is only a fallback for producers that omit it.
func TestExtract3DModelsReadsStreamSubtype(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ subtype, want string }{
		{"/Subtype /U3D", "U3D"},
		{"/Subtype /PRC", "PRC"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()

			data := model3DPDF(
				"<< /Type /Annot /Subtype /3D /Rect [0 0 20 20] /3DD 5 0 R >>",
				streamObj(tc.subtype, "payload bytes"),
			)
			got := extractModels(t, data)
			if len(got) != 1 {
				t.Fatalf("got %d model(s), want 1", len(got))
			}
			if got[0].Subtype != tc.want {
				t.Errorf("subtype %q, want %q", got[0].Subtype, tc.want)
			}
		})
	}
}

// TestExtract3DModelsFallsBackToMagicBytes pins the recovery path for a stream
// that names /Type /3D without a /Subtype. The format is then read from the
// leading bytes, which is how a PRC stream is told from a U3D one when the
// producer left the dictionary entry out.
func TestExtract3DModelsFallsBackToMagicBytes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, payload, want string }{
		{"PRCHeader", "PRC and then some", "PRC"},
		{"OtherHeader", "not a prc header", "U3D"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data := model3DPDF(
				"<< /Type /Annot /Subtype /3D /Rect [0 0 20 20] /3DD 5 0 R >>",
				streamObj("/Type /3D", tc.payload),
			)
			got := extractModels(t, data)
			if len(got) != 1 {
				t.Fatalf("got %d model(s), want 1", len(got))
			}
			if got[0].Subtype != tc.want {
				t.Errorf("subtype %q, want %q for payload %q", got[0].Subtype, tc.want, tc.payload)
			}
		})
	}
}

// TestExtract3DModelsNamesFromNM pins that the annotation's /NM supplies the
// model name and that the positional default fills in when it is absent. The
// default carries the index, so two unnamed models stay distinguishable.
func TestExtract3DModelsNamesFromNM(t *testing.T) {
	t.Parallel()

	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R 6 0 R] >>",
		"<< /Type /Annot /Subtype /3D /Rect [0 0 20 20] /NM (turbine) /3DD 5 0 R >>",
		streamObj("/Subtype /U3D", "first payload"),
		"<< /Type /Annot /Subtype /3D /Rect [0 0 20 20] /3DD 7 0 R >>",
		streamObj("/Subtype /PRC", "second payload"),
	})
	got := extractModels(t, data)
	if len(got) != 2 {
		t.Fatalf("got %d model(s), want 2", len(got))
	}
	if got[0].Name != "turbine" {
		t.Errorf("first model name %q, want %q from /NM", got[0].Name, "turbine")
	}
	if got[1].Name != "model_1" {
		t.Errorf("second model name %q, want the positional default %q", got[1].Name, "model_1")
	}
}

// TestExtract3DModelsOpensLazily pins the documented contract that the stream
// data is not decoded until Open is called, and that Open then yields the
// stream's bytes. A 3D model can be very large, so extraction itself must stay
// cheap.
func TestExtract3DModelsOpensLazily(t *testing.T) {
	t.Parallel()

	const payload = "hello 3d model"
	data := model3DPDF(
		"<< /Type /Annot /Subtype /3D /Rect [0 0 20 20] /3DD 5 0 R >>",
		streamObj("/Subtype /U3D", payload),
	)
	got := extractModels(t, data)
	if len(got) != 1 {
		t.Fatalf("got %d model(s), want 1", len(got))
	}
	if got[0].Open == nil {
		t.Fatal("Open accessor is nil")
	}
	rc, err := got[0].Open(context.Background())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if err := rc.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(body) != payload {
		t.Errorf("Open yielded %q, want %q", body, payload)
	}
}
