package pdf

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// buildPDF writes objs as objects 1 to n of a PDF 1.7 file with the binary
// header comment and an exact cross-reference table, so a remediation repairs
// it in an incremental update. Object 1 is the catalog.
func buildPDF(objs []string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// mixedColorPDF builds a page painting in DeviceRGB and DeviceCMYK through an
// indirect resource dictionary. A PDF/A-2b remediation adds an output intent
// for one of the two spaces and a Default colour space for the other into that
// dictionary, at its own object number (ISO 19005-2, 6.2.4.3).
func mixedColorPDF() []byte {
	const content = "1 0 0 rg 10 10 50 50 re f 0 0 0 1 k 100 100 50 50 re f\n" +
		"BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources 6 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Font << /F1 5 0 R >> >>",
	})
}

// placeholderFormPDF builds a page with one text field whose form asks the
// viewer to build appearances (ISO 32000-1, 12.7.3.3), whose stored appearance
// paints nothing, and whose widget carries a JavaScript action. A PDF/A-2b
// remediation clears the flag ISO 19005-2, 6.4.1 forbids, in the form
// dictionary at its own object number, and the action 6.4.1 and 6.5.1 forbid,
// in the widget at its own.
func placeholderFormPDF() []byte {
	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	const ap = "/Tx BMC EMC\n"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 6 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> /Annots [7 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Fields [7 0 R] /NeedAppearances true /DA (/Helv 0 Tf 0 g) >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (value) /F 4 /P 3 0 R" +
			" /Rect [100 600 300 620] /AP << /N 8 0 R >> /A << /S /JavaScript /JS (app.alert\\(1\\)) >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 20] /Length %d >>\nstream\n%s\nendstream", len(ap), ap),
	})
}

// ruleHits returns the clause and test of each violation, sorted, so two
// outputs that number their new objects differently still compare.
func ruleHits(errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, v := range toViolations(errs) {
		out = append(out, fmt.Sprintf("%s t%d", v.Clause, v.TestNumber))
	}
	slices.Sort(out)
	return out
}

// TestWriterEditAfterOneRemediationRoundMatchesEditingItsOutput pins that an
// edit queued after a remediation that keeps one round yields what the same
// edit yields on the remediated document: every edit builds on the repairs.
//
// A remediation that keeps one round leaves its repairs in the Writer's
// overrides, where an edit reads them only if it reads alloc first. Each edit
// here read the source's copy of an object the remediation had rewritten at
// its own number and wrote it back unrepaired. A stamp merged the source's
// resource dictionary into the stamped page, losing the Default colour space
// ISO 19005-2, 6.2.4.3 required. A flatten read the source's form, which still
// asked for appearance regeneration, kept the widget, and wrote back its
// source copy with the JavaScript action the remediation had removed. A form
// field edit rewrote the form from the source's copy.
func TestWriterEditAfterOneRemediationRoundMatchesEditingItsOutput(t *testing.T) {
	t.Parallel()

	pngData := buildPNG(t, false)
	for _, tc := range []struct {
		name string
		data []byte
		edit func(ctx context.Context, w *Writer) error
	}{
		{
			name: "a text stamp",
			data: mixedColorPDF(),
			edit: func(_ context.Context, w *Writer) error {
				return w.StampText(TextStamp{Text: "DRAFT", X: 50, Y: 50})
			},
		},
		{
			name: "an image stamp",
			data: mixedColorPDF(),
			edit: func(_ context.Context, w *Writer) error {
				return w.StampImage(ImageStamp{Data: pngData, MimeType: "image/png", X: 10, Y: 10, Width: 20})
			},
		},
		{
			name: "a PDF stamp",
			data: mixedColorPDF(),
			edit: func(ctx context.Context, w *Writer) error {
				stamp := signablePDF()
				r, err := Open(ctx, bytes.NewReader(stamp), int64(len(stamp)))
				if err != nil {
					return err
				}
				return w.StampPDF(PDFStamp{Source: r, Page: 1, Scale: 0.2})
			},
		},
		{
			name: "a form flatten",
			data: placeholderFormPDF(),
			edit: func(_ context.Context, w *Writer) error {
				w.FlattenForm()
				return nil
			},
		},
		{
			name: "a form field edit",
			data: placeholderFormPDF(),
			edit: func(_ context.Context, w *Writer) error {
				w.SetFormField("name", "other")
				return nil
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			rounds := &keptRounds{}
			r, err := Open(ctx, bytes.NewReader(tc.data), int64(len(tc.data)), WithLogger(slog.New(rounds)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			report, err := w.RemediatePDFA(ctx, models.PDFA2b)
			if err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			if rounds.n.Load() != 0 {
				t.Fatalf("the remediation kept %d later rounds, so the Writer edits its output and the test proves nothing", rounds.n.Load())
			}
			if len(report.Fixed) == 0 {
				t.Fatal("the remediation fixed nothing, so the test proves nothing")
			}

			// The remediated document, edited on its own.
			var remediated bytes.Buffer
			if err := w.WriteTo(ctx, &remediated); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			base, err := Open(ctx, bytes.NewReader(remediated.Bytes()), int64(remediated.Len()))
			if err != nil {
				t.Fatalf("reopening the remediated output: %v", err)
			}
			defer base.Close()
			wantW := NewWriter(base)
			defer wantW.Close()
			if err := tc.edit(ctx, wantW); err != nil {
				t.Fatalf("editing the remediated output: %v", err)
			}
			var want bytes.Buffer
			if err := wantW.WriteTo(ctx, &want); err != nil {
				t.Fatalf("WriteTo of the edited remediated output: %v", err)
			}

			// The same edit queued after the remediation.
			r2, err := Open(ctx, bytes.NewReader(tc.data), int64(len(tc.data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r2.Close()
			gotW := NewWriter(r2)
			defer gotW.Close()
			if _, err := gotW.RemediatePDFA(ctx, models.PDFA2b); err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			if err := tc.edit(ctx, gotW); err != nil {
				t.Fatalf("editing after the remediation: %v", err)
			}
			var got bytes.Buffer
			if err := gotW.WriteTo(ctx, &got); err != nil {
				t.Fatalf("WriteTo of the edit queued after the remediation: %v", err)
			}

			gotHits, wantHits := ruleHits(validatePDFA2b(t, got.Bytes())), ruleHits(validatePDFA2b(t, want.Bytes()))
			if !slices.Equal(gotHits, wantHits) {
				t.Errorf("the edit queued after the remediation validates to %q, want the edited remediated output's %q", gotHits, wantHits)
			}
		})
	}
}
