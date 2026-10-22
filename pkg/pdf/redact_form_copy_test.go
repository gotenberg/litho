package pdf

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// placeFormRight draws /Fm1 where placeRight draws /Im1.
const placeFormRight = "q 1 0 0 1 300 500 cm /Fm1 Do Q"

// formChainDoc builds formImageDoc's document with depth forms nested
// between the page and the image: the page draws the outermost as /Fm1, each
// form draws the next as /Fm1, and the innermost, object 9, draws /Im1 over
// its 100pt square. Outer forms are objects 20, 21, ....
func formChainDoc(depth int, contents []string) []byte {
	src := formImageDoc(contents)
	if depth <= 1 {
		return src
	}
	body := "/Fm1 Do"
	var forms strings.Builder
	for level := 1; level < depth; level++ {
		inner := 9
		if level < depth-1 {
			inner = 20 + level
		}
		forms.WriteString(strconv.Itoa(20+level-1) + " 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] " +
			"/Resources << /XObject << /Fm1 " + strconv.Itoa(inner) + " 0 R >> >> /Length " + strconv.Itoa(len(body)) +
			" >>\nstream\n" + body + "\nendstream\nendobj\n")
	}
	src = bytes.Replace(src, []byte("/Fm1 9 0 R >>"), []byte("/Fm1 20 0 R >>"), 1)
	return bytes.Replace(src, []byte("trailer\n"), []byte(forms.String()+"trailer\n"), 1)
}

// TestWriter_Redact_BlanksASharedFormImageInAFormCopy pins that an image a
// form draws on two pages, under a region on one of them, is blanked in a
// copy of the form only the covered placement draws: the other page keeps
// every sample, and the report names the image and counts the form copy.
func TestWriter_Redact_BlanksASharedFormImageInAFormCopy(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		region       [4]float64
		top, bottom  uint8
		wantCopyName string
	}{
		{"whole placement", wholeLeft, 0, 0, "Im1"},
		{"top half", topLeft, 0, 0x40, "Im1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := formImageDoc([]string{placeFormLeft, placeFormLeft})
			out, report, err := redactWith(t, src, []RedactRegion{{Page: 1, Rect: tc.region}})
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if got := report.ImagesBlankedInCopy[1]; !slices.Equal(got, []string{tc.wantCopyName}) {
				t.Errorf("ImagesBlankedInCopy = %v, want [%s] on page 1", report.ImagesBlankedInCopy, tc.wantCopyName)
			}
			if got := report.FormsCopied; len(got) != 1 || got[1] != 1 {
				t.Errorf("FormsCopied = %v, want one form on page 1", got)
			}
			if got := renderedGray(t, out, 1, copyLeftTop); !nearGray(got, tc.top) {
				t.Errorf("page 1 top row = %d, want %d", got, tc.top)
			}
			if got := renderedGray(t, out, 1, copyLeftBottom); !nearGray(got, tc.bottom) {
				t.Errorf("page 1 bottom row = %d, want %d", got, tc.bottom)
			}
			for _, at := range [][2]float64{copyLeftTop, copyLeftBottom} {
				if got := renderedGray(t, out, 2, at); !nearGray(got, 0x40) {
					t.Errorf("page 2 at %v = %d, want 0x40", at, got)
				}
			}
		})
	}
}

// TestWriter_Redact_FormCopyPerPlacementOnOnePage pins that a form drawn
// twice on one page, one placement under the region, is copied for that
// placement alone: the other keeps drawing the form and its image whole.
func TestWriter_Redact_FormCopyPerPlacementOnOnePage(t *testing.T) {
	t.Parallel()

	src := formImageDoc([]string{placeFormLeft + " " + placeFormRight})
	out, report, err := redactWith(t, src, []RedactRegion{{Page: 1, Rect: wholeLeft}})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if got := report.FormsCopied[1]; got != 1 {
		t.Errorf("FormsCopied[1] = %d, want 1", got)
	}
	if got := renderedGray(t, out, 1, copyLeftTop); !nearGray(got, 0) {
		t.Errorf("covered placement = %d, want 0", got)
	}
	if got := renderedGray(t, out, 1, copyRight); !nearGray(got, 0x40) {
		t.Errorf("placement outside the region = %d, want 0x40", got)
	}
}

// TestWriter_Redact_FormCopyFollowsNestedForms pins that an image drawn
// through nested forms is blanked in a copy of every form of the chain, so
// the other page, drawing the same outermost form, keeps the image whole.
func TestWriter_Redact_FormCopyFollowsNestedForms(t *testing.T) {
	t.Parallel()

	for _, depth := range []int{2, 3, 5} {
		t.Run(strconv.Itoa(depth), func(t *testing.T) {
			t.Parallel()
			src := formChainDoc(depth, []string{placeFormLeft + " " + placeFormRight, placeFormLeft})
			out, report, err := redactWith(t, src, []RedactRegion{{Page: 1, Rect: wholeLeft}})
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if got := report.FormsCopied[1]; got != depth {
				t.Errorf("FormsCopied[1] = %d, want %d", got, depth)
			}
			if got := renderedGray(t, out, 1, copyLeftTop); !nearGray(got, 0) {
				t.Errorf("covered placement = %d, want 0", got)
			}
			if got := renderedGray(t, out, 1, copyRight); !nearGray(got, 0x40) {
				t.Errorf("page 1 placement outside the region = %d, want 0x40", got)
			}
			if got := renderedGray(t, out, 2, copyLeftTop); !nearGray(got, 0x40) {
				t.Errorf("page 2 = %d, want 0x40", got)
			}
		})
	}
}

// TestWriter_Redact_FormCopyRepointsOnlyTheCoveredDo pins that a form drawing
// its image twice, one placement under the region, is copied with only that
// Do repointed: the copy still draws the image whole at the other.
func TestWriter_Redact_FormCopyRepointsOnlyTheCoveredDo(t *testing.T) {
	t.Parallel()

	src := formImageDoc([]string{placeFormLeft})
	src = bytes.Replace(src, []byte("/Length 30 >>\nstream\nq 100 0 0 100 0 0 cm /Im1 Do Q"),
		[]byte("/Length 60 >>\nstream\nq 50 0 0 100 0 0 cm /Im1 Do Q q 50 0 0 100 50 0 cm /Im1 Do Q"), 1)
	out, report, err := redactWith(t, src, []RedactRegion{{Page: 1, Rect: [4]float64{90, 490, 145, 610}}})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if got := report.FormsCopied[1]; got != 1 {
		t.Errorf("FormsCopied[1] = %d, want 1", got)
	}
	if got := renderedGray(t, out, 1, [2]float64{112, 550}); !nearGray(got, 0) {
		t.Errorf("covered placement = %d, want 0", got)
	}
	if got := renderedGray(t, out, 1, [2]float64{175, 550}); !nearGray(got, 0x40) {
		t.Errorf("the form's other placement = %d, want 0x40", got)
	}
}

// TestWriter_WriteTo_ReappliesAFormCopyThatWasUndone pins that WriteTo blanks
// a placement again when a later edit puts the page back drawing the form
// itself, whose image the redaction never touched.
func TestWriter_WriteTo_ReappliesAFormCopyThatWasUndone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	src := formImageDoc([]string{placeFormLeft, placeFormLeft})
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	if _, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: wholeLeft}}); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	pages, err := w.reader.resolvePages(ctx)
	if err != nil {
		t.Fatalf("resolvePages: %v", err)
	}
	w.rewriteOverrides[pages[0].Ref.ObjectNum] = pages[0].Dict

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got := renderedGray(t, out.Bytes(), 1, copyLeftTop); !nearGray(got, 0) {
		t.Errorf("covered placement = %d, want 0", got)
	}
	if got := renderedGray(t, out.Bytes(), 2, copyLeftTop); !nearGray(got, 0x40) {
		t.Errorf("page 2 = %d, want 0x40", got)
	}
}

// TestWriter_Redact_FormCopyWithTheOtherOptions pins that a shared form image
// is redacted under the raster fallback and the pixel check: the fallback
// takes the form for the covered placement, the pixel check reads the form
// copy as blanked, and the other page keeps the image whole either way.
func TestWriter_Redact_FormCopyWithTheOtherOptions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		opt    RedactOption
		raster bool
	}{
		{"raster fallback", WithRedactionRasterFallback(), true},
		{"pixel check", WithRedactionPixelCheck(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := formImageDoc([]string{placeFormLeft + " " + placeRight, placeFormLeft})
			out, report, err := redactWith(t, src, []RedactRegion{{Page: 1, Rect: wholeLeft}}, tc.opt)
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if tc.raster && report.RasterisedUnits == 0 {
				t.Error("RasterisedUnits = 0, want the form rasterised")
			}
			if !tc.raster && report.FormsCopied[1] != 1 {
				t.Errorf("FormsCopied = %v, want one form on page 1", report.FormsCopied)
			}
			if got := renderedGray(t, out, 1, copyLeftTop); nearGray(got, 0x40) {
				t.Errorf("covered placement = %d, still the image's 0x40", got)
			}
			if got := renderedGray(t, out, 1, copyRight); !nearGray(got, 0x80) {
				t.Errorf("the page's own image outside the region = %d, want 0x80", got)
			}
			if got := renderedGray(t, out, 2, copyLeftTop); !nearGray(got, 0x40) {
				t.Errorf("page 2 = %d, want 0x40", got)
			}
		})
	}
}

// TestWriter_Redact_FormCopyIsDeterministic pins that a redaction copying
// forms writes the same bytes on every run.
func TestWriter_Redact_FormCopyIsDeterministic(t *testing.T) {
	t.Parallel()

	src := formChainDoc(3, []string{placeFormLeft + " " + placeFormRight, placeFormLeft})
	regions := []RedactRegion{{Page: 1, Rect: topLeft}, {Page: 1, Rect: wholeRight}, {Page: 2, Rect: bottomLeft}}
	first, _, err := redactWith(t, src, regions)
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	for range 3 {
		again, _, err := redactWith(t, src, regions)
		if err != nil {
			t.Fatalf("Redact: %v", err)
		}
		if !bytes.Equal(first, again) {
			t.Fatal("two runs over the same input wrote different bytes")
		}
	}
}

// TestWriter_Redact_FormCopiesPastTheCapRefuse pins that a page whose
// covered placements would copy more forms than the per-page cap refuses
// rather than copying without bound, and leaves the document as it was.
func TestWriter_Redact_FormCopiesPastTheCapRefuse(t *testing.T) {
	t.Parallel()

	// At the cap every placement gets its copy.
	atCap := formImageDoc([]string{strings.Repeat(placeFormLeft+" ", 1024), placeFormLeft})
	if _, report, err := redactWith(t, atCap, []RedactRegion{{Page: 1, Rect: wholeLeft}}); err != nil || report.FormsCopied[1] != 1024 {
		t.Fatalf("Redact at the cap = %v, %v, want 1024 forms copied on page 1", err, report)
	}

	page1 := strings.Repeat(placeFormLeft+" ", 1025)
	src := formImageDoc([]string{page1, placeFormLeft})
	out, _, err := redactWith(t, src, []RedactRegion{{Page: 1, Rect: wholeLeft}})
	if !errors.Is(err, ErrRedactionRefused) || !errors.Is(err, mutations.ErrImageShared) {
		t.Fatalf("Redact = %v, want ErrRedactionRefused wrapping ErrImageShared", err)
	}
	if got := renderedGray(t, out, 1, copyLeftTop); !nearGray(got, 0x40) {
		t.Errorf("page 1 after a refusal = %d, want 0x40", got)
	}
}
