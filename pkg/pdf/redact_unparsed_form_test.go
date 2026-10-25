package pdf

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// unparsedFormBody is what the second form, object 30, draws: formImageDoc's
// /Im1 over its 100pt square.
const unparsedFormBody = "q 100 0 0 100 0 0 cm /Im1 Do Q"

// placeUnparsedRight draws the second form, /Fm2, where placeRight draws /Im1.
const placeUnparsedRight = "q 1 0 0 1 300 500 cm /Fm2 Do Q"

// unparsedFormDoc builds formImageDoc's one-page document, whose form /Fm1
// draws /Im1 at the left, with a second form /Fm2 at the right that names
// the same image in its own resources. dict and data are the extra entries
// and the raw data of /Fm2's content stream.
func unparsedFormDoc(dict string, data []byte) []byte {
	src := formImageDoc([]string{placeFormLeft + " " + placeUnparsedRight})
	src = bytes.Replace(src, []byte("/Fm1 9 0 R >>"), []byte("/Fm1 9 0 R /Fm2 30 0 R >>"), 1)
	form := "30 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /XObject << /Im1 8 0 R >> >> " +
		dict + " /Length " + strconv.Itoa(len(data)) + " >>\nstream\n" + string(data) + "\nendstream\nendobj\n"
	return bytes.Replace(src, []byte("trailer\n"), []byte(form+"trailer\n"), 1)
}

// deflated returns data compressed for /FlateDecode.
func deflated(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatalf("deflating: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("deflating: %v", err)
	}
	return buf.Bytes()
}

// TestWriter_Redact_CopiesAnImageAnUnparsedFormNames pins that an image a
// form whose content litho cannot parse also names is blanked in a copy,
// never in place, and refused under strict redaction.
//
// The run that measures placements read such a form as painting nothing, so
// the only placement it measured was the covered one, and the image was
// blanked in place: a viewer that reads the form more leniently than litho
// paints the image from it outside every region, and showed the blank there.
func TestWriter_Redact_CopiesAnImageAnUnparsedFormNames(t *testing.T) {
	t.Parallel()

	// A string literal past the lexer's 10 MiB bound fails the parse under
	// every option; a viewer reads the Do before it.
	oversized := append([]byte(unparsedFormBody+" ("), bytes.Repeat([]byte{'A'}, 10<<20+1)...)
	oversized = append(oversized, ')')
	whole := deflated(t, []byte(unparsedFormBody))

	for _, tc := range []struct {
		name string
		dict string
		data []byte
		open []OpenOption
	}{
		// Strict streams refuse the salvaged data of a stream cut short.
		{"truncated, strict streams", "/Filter /FlateDecode", whole[:len(whole)/2], []OpenOption{WithStrictStreams()}},
		{"oversized string", "/Filter /FlateDecode", deflated(t, oversized), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := unparsedFormDoc(tc.dict, tc.data)
			regions := []RedactRegion{{Page: 1, Rect: wholeLeft}}

			out, report, err := redactOpened(t, src, tc.open, regions)
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if got := report.ImagesBlankedInCopy[1]; !slices.Equal(got, []string{"Im1"}) {
				t.Errorf("ImagesBlankedInCopy = %v, want [Im1] on page 1", report.ImagesBlankedInCopy)
			}
			if got := renderedGray(t, out, 1, copyLeftTop); !nearGray(got, 0) {
				t.Errorf("covered placement = %d, want 0", got)
			}
			if !keepsFormSamples(t, out) {
				t.Error("no image keeps /Im1's samples for the form litho cannot parse")
			}

			if _, _, err := redactOpened(t, src, tc.open, regions, WithStrictRedaction()); !errors.Is(err, mutations.ErrImageShared) {
				t.Errorf("strict Redact = %v, want ErrImageShared", err)
			}
		})
	}
}

// TestWriter_Redact_UnparsedFormUnderARegion pins what a region reaching a
// form litho cannot parse does to an image the form names. The form's /BBox
// clips what it paints, so a form inside a region shows only covered samples
// and the image, covered whole elsewhere, is blanked in place. A region that
// reaches only part of the form leaves which samples it shows unknown: the
// image is blanked in a copy, the form still paints the original under the
// region, and the redaction refuses. The redaction pixel check writes the
// document, which is where that refusal surfaces.
func TestWriter_Redact_UnparsedFormUnderARegion(t *testing.T) {
	t.Parallel()

	whole := deflated(t, []byte(unparsedFormBody))
	src := unparsedFormDoc("/Filter /FlateDecode", whole[:len(whole)/2])
	open := []OpenOption{WithStrictStreams()}

	t.Run("inside a region", func(t *testing.T) {
		t.Parallel()
		regions := []RedactRegion{{Page: 1, Rect: wholeLeft}, {Page: 1, Rect: [4]float64{290, 490, 410, 610}}}
		out, report, err := redactOpened(t, src, open, regions, WithStrictRedaction())
		if err != nil {
			t.Fatalf("Redact: %v", err)
		}
		if len(report.ImagesBlankedInCopy) != 0 || len(report.FormsCopied) != 0 {
			t.Errorf("ImagesBlankedInCopy = %v, FormsCopied = %v, want none", report.ImagesBlankedInCopy, report.FormsCopied)
		}
		if keepsFormSamples(t, out) {
			t.Error("an image keeps /Im1's samples, want it blanked in place")
		}
	})

	t.Run("partly under a region", func(t *testing.T) {
		t.Parallel()
		regions := []RedactRegion{{Page: 1, Rect: wholeLeft}, {Page: 1, Rect: [4]float64{350, 490, 410, 610}}}
		if _, _, err := redactOpened(t, src, open, regions); !errors.Is(err, ErrRedactionIncomplete) {
			t.Errorf("Redact = %v, want ErrRedactionIncomplete", err)
		}
	})
}

// TestWriter_Redact_BlanksInPlaceBesideAParsedForm is the control of
// [TestWriter_Redact_CopiesAnImageAnUnparsedFormNames]: the same second form,
// whose content parses and draws nothing, leaves the image blanked in place.
func TestWriter_Redact_BlanksInPlaceBesideAParsedForm(t *testing.T) {
	t.Parallel()

	src := unparsedFormDoc("/Filter /FlateDecode", deflated(t, []byte("% nothing drawn")))
	out, report, err := redactOpened(t, src, nil, []RedactRegion{{Page: 1, Rect: wholeLeft}}, WithStrictRedaction())
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if len(report.ImagesBlankedInCopy) != 0 || len(report.FormsCopied) != 0 {
		t.Errorf("ImagesBlankedInCopy = %v, FormsCopied = %v, want none", report.ImagesBlankedInCopy, report.FormsCopied)
	}
	if keepsFormSamples(t, out) {
		t.Error("an image keeps /Im1's samples, want it blanked in place")
	}
}

// redactOpened is [redactWith] for a document opened with open.
func redactOpened(t *testing.T, src []byte, open []OpenOption, regions []RedactRegion, opts ...RedactOption) ([]byte, *RedactReport, error) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)), open...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	report, redactErr := w.Redact(ctx, regions, opts...)
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes(), report, redactErr
}

// keepsFormSamples reports whether an image XObject of pdf still holds the
// 16 samples of 0x40 that formImageDoc's /Im1, object 8, starts with: the
// writer renumbers objects, so the image is found by its samples.
func keepsFormSamples(t *testing.T, pdf []byte) bool {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := bytes.Repeat([]byte{0x40}, 16)
	for objNum := 1; objNum <= r.doc.HighestObjectNumber(ctx); objNum++ {
		stream, err := document.ResolveStream(ctx, r.doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil || stream.Dict["Subtype"] != ast.PdfName("Image") {
			continue
		}
		data, err := r.doc.DecodeStream(ctx, stream)
		if err == nil && bytes.Equal(data, want) {
			return true
		}
	}
	return false
}
