package flatten

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
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

// intentPage returns page dictionary entries giving the page an output
// intent whose destination profile, of n components, is object 7, and that
// object.
func intentPage(profile []byte, n int) (string, string) {
	return "/OutputIntents [<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (Test) /DestOutputProfile 7 0 R >>]",
		stream(fmt.Sprintf("/N %d", n), string(profile))
}

// TestFlattenCompositesInTheOutputIntentSpace checks that a page group that
// names no /CS composites in the device space the output intent
// characterises (ISO 32000-1, 11.4.7, Table 147, 14.11.5): a grey intent
// gives DeviceGray composites, a CMYK one DeviceCMYK, and an RGB one the
// sRGB composites of a document with no intent.
func TestFlattenCompositesInTheOutputIntentSpace(t *testing.T) {
	t.Parallel()
	// The CMYK page paints black ink alone, which blends in CMYK as the
	// renderer of the source blends it in sRGB, within a few levels: other
	// colours blend apart, as a print of the page blends them.
	const grey, cmyk = "0.2 g 0 0 100 100 re f q /GS1 gs 0.8 g 10 10 30 30 re f Q",
		"0 0 0 0.8 k 0 0 100 100 re f q /GS1 gs 0 0 0 0.2 k 10 10 30 30 re f Q"
	for _, tc := range []struct {
		profile []byte
		want    ast.PdfName
		n       int
		content string
	}{
		{icc.SGrayProfile(), "DeviceGray", 1, grey},
		{icc.CMYKPrinterProfile(), "DeviceCMYK", 4, cmyk},
		{icc.SRGBProfile(), "", 3, grey},
	} {
		page, obj := intentPage(tc.profile, tc.n)
		out, _ := checkFlattened(t, fixture{
			page:    page,
			content: tc.content,
			objs:    []string{obj},
		})
		dict, samples := compositeImage(t, out)
		if tc.want == "" {
			if _, ok := dict["ColorSpace"].(ast.PdfArray); !ok {
				t.Errorf("RGB intent: composite colour space %v, want the sRGB ICCBased space", dict["ColorSpace"])
			}
		} else if cs, _ := dict["ColorSpace"].(ast.PdfName); cs != tc.want {
			t.Errorf("composite colour space %v, want %s", dict["ColorSpace"], tc.want)
		}
		w, _ := dict["Width"].(ast.PdfInt)
		h, _ := dict["Height"].(ast.PdfInt)
		if len(samples) != tc.n*int(w)*int(h) {
			t.Errorf("%d components: %d samples for a %dx%d image", tc.n, len(samples), w, h)
		}
	}
}

// TestFlattenKeepsTheGroupSpaceItNames checks that a page group's own /CS
// wins over the output intent's device.
func TestFlattenKeepsTheGroupSpaceItNames(t *testing.T) {
	t.Parallel()
	page, obj := intentPage(icc.SGrayProfile(), 1)
	out, _ := checkFlattened(t, fixture{
		page:    page + " /Group << /S /Transparency /CS /DeviceCMYK >>",
		content: "0 0 0 1 k 0 0 100 100 re f q /GS1 gs 1 0 0 0 k 10 10 30 30 re f Q",
		objs:    []string{obj},
	})
	dict, _ := compositeImage(t, out)
	if cs, _ := dict["ColorSpace"].(ast.PdfName); cs != "DeviceCMYK" {
		t.Errorf("composite colour space %v, want the group's DeviceCMYK", dict["ColorSpace"])
	}
}
