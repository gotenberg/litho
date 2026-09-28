package pdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/models"
)

// determinismRuns is how many times the determinism guards produce each
// output. Two runs catch a Go map iterated into ordered output about as often
// as any number does, since each run draws a fresh iteration seed; the third
// runs beside the others and so meets another schedule of the concurrent
// compliance check groups.
const determinismRuns = 3

// litho corpus fixtures the determinism guards read. They are tracked in the
// repository, unlike the other corpora.
const lithoFixtureDir = "../../test/integration/corpus/litho/pdfs"

// pinnedDocumentID is the /ID seed the remediation guard pins, so the one
// documented nondeterministic input stays out of the comparison.
var pinnedDocumentID = []byte("LITHO-PINNED-ID!")

// readLithoFixture returns the named litho corpus fixture.
func readLithoFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(lithoFixtureDir, name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return data
}

// sameOutputEveryRun calls produce [determinismRuns] times, concurrently, and
// fails when two calls return different bytes or any call fails.
func sameOutputEveryRun(t *testing.T, produce func() ([]byte, error)) {
	t.Helper()
	outs := make([][]byte, determinismRuns)
	errs := make([]error, determinismRuns)
	var wg sync.WaitGroup
	for i := range determinismRuns {
		wg.Go(func() { outs[i], errs[i] = produce() })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	for i := 1; i < determinismRuns; i++ {
		if !bytes.Equal(outs[0], outs[i]) {
			t.Fatalf("run %d wrote %x, run 0 wrote %x: the same input and options gave different bytes",
				i, sha256.Sum256(outs[i]), sha256.Sum256(outs[0]))
		}
	}
}

// remediateFixture remediates data at profile with opts and returns the bytes
// written followed by the report, so a residual or a fix that changes from run
// to run fails the comparison even when the document bytes do not.
func remediateFixture(ctx context.Context, data []byte, profile string, opts ...RemediateOption) ([]byte, error) {
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("opening: %w", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()

	var report *RemediationReport
	switch {
	case strings.HasPrefix(profile, "PDF/A"):
		report, err = w.RemediatePDFA(ctx, models.PDFAProfile(profile), opts...)
	case strings.HasPrefix(profile, "PDF/UA"):
		report, err = w.RemediatePDFUA(ctx, models.PDFUAProfile(profile), opts...)
	default:
		report, err = w.RemediateWTPDF(ctx, models.WTPDFProfile(profile), opts...)
	}
	if err != nil {
		return nil, fmt.Errorf("remediating: %w", err)
	}
	if len(report.Fixed) == 0 {
		return nil, fmt.Errorf("remediation fixed nothing, so the comparison would prove nothing")
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		return nil, fmt.Errorf("writing: %w", err)
	}
	for _, e := range report.Residual {
		out.WriteString("\nresidual: " + e.Error())
	}
	for _, f := range report.Fixed {
		out.WriteString("\nfixed: " + f)
	}
	for _, f := range report.RolledBack {
		out.WriteString("\nrolled back: " + f)
	}
	for _, f := range report.FontRefusals {
		out.WriteString("\nrefused font: " + f.String())
	}
	return out.Bytes(), nil
}

// TestRemediationWritesTheSameBytesEveryRun guards CONTRIBUTING "Deterministic
// output" for remediation: with the /ID pinned, the same fixture remediated at
// the same profile writes the same bytes and the same report on every run, in
// the default and the aggressive mode, and with a font provider.
//
// # Corpus evidence
//
//   - pdfjs: remediated three times per file at PDF/UA-1, PDF/UA-2 and
//     WTPDF-1.0-Accessibility, default and aggressive, with the /ID pinned,
//     every one of 7212 cells wrote the same bytes and report.
//   - litho: every fixture at 15 profiles, both modes, under a delay injected
//     at random before each compliance check group and a 64-object cache,
//     wrote the same bytes in all 4080 cells.
//   - govdocs, verapdf, pdfcpu, pdf20, facturx: the 930 files naming a
//     /StructTreeRoot in their bytes, at the same 15 profiles and both modes
//     under the same injected delay, wrote the same bytes in all 27900 cells.
//   - pdfjs: the command line wrote different bytes for 7 of 158 tagged
//     files at WTPDF-1.0-Accessibility, all in the trailer /ID alone, the
//     random /ID remediation writes for a file without a valid one.
func TestRemediationWritesTheSameBytesEveryRun(t *testing.T) {
	t.Parallel()

	fonts := WithFontFiles(fstest.MapFS{
		"Arial.ttf": {Data: abProgram("ArialMT", "Arial", "Regular", 667, 667)},
	})
	cases := []struct {
		fixture, profile string
		opts             []RemediateOption
	}{
		{"complex_2a.pdf", "PDF/A-2a", nil},
		{"complex_4.pdf", "PDF/A-4", nil},
		{"complex_ua1.pdf", "PDF/UA-1", nil},
		{"complex_ua2.pdf", "PDF/UA-2", nil},
		{"complex_ua2.pdf", "WTPDF-1.0-Accessibility", nil},
		{"structcopy_ua2.pdf", "WTPDF-1.0-Accessibility", nil},
		{"annotshare_ua1.pdf", "PDF/UA-1", nil},
		{"annotkinds_ua2.pdf", "PDF/UA-2", nil},
		{"multipage_2b.pdf", "PDF/A-2b", nil},
		{"fontcopies_2b.pdf", "PDF/A-2b", []RemediateOption{fonts}},
	}
	for _, tc := range cases {
		for _, aggressive := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%s/aggressive=%v", tc.fixture, tc.profile, aggressive), func(t *testing.T) {
				t.Parallel()
				data := readLithoFixture(t, tc.fixture)
				opts := append([]RemediateOption{WithDocumentID(pinnedDocumentID)}, tc.opts...)
				if aggressive {
					opts = append(opts, WithAggressiveFixes())
				}
				sameOutputEveryRun(t, func() ([]byte, error) {
					return remediateFixture(context.Background(), data, tc.profile, opts...)
				})
			})
		}
	}
}

// TestWriterEditsWriteTheSameBytesEveryRun guards CONTRIBUTING "Deterministic
// output" for the Writer's edits, Split and Merge: each writes the same bytes
// on every run over the same fixture.
//
// # Corpus evidence
//
//   - litho, pdfjs: 15 edits, Split and Merge among them, run three times
//     over every litho fixture and 158 tagged pdfjs files wrote the same
//     bytes in all 4410 cells.
func TestWriterEditsWriteTheSameBytesEveryRun(t *testing.T) {
	t.Parallel()

	edits := map[string]func(ctx context.Context, r *Reader, out *bytes.Buffer) error{
		"stamp": func(ctx context.Context, r *Reader, out *bytes.Buffer) error {
			return writeEdited(ctx, r, out, func(w *Writer) error {
				return w.StampText(TextStamp{Text: "DRAFT", X: 100, Y: 100, FontSize: 30, Opacity: 0.5, Rotation: 45})
			})
		},
		"autotag": func(ctx context.Context, r *Reader, out *bytes.Buffer) error {
			return writeEdited(ctx, r, out, func(w *Writer) error { w.AutoTag(); return nil })
		},
		"bookmarks": func(ctx context.Context, r *Reader, out *bytes.Buffer) error {
			return writeEdited(ctx, r, out, func(w *Writer) error {
				w.GenerateBookmarks(BookmarkGenerationOptions{}, BookmarkReplace)
				return nil
			})
		},
		"insert": func(ctx context.Context, r *Reader, out *bytes.Buffer) error {
			return writeEdited(ctx, r, out, func(w *Writer) error { return w.InsertPages(0, r, 2, 1) })
		},
		"compact": func(ctx context.Context, r *Reader, out *bytes.Buffer) error {
			w := NewWriter(r)
			defer w.Close()
			return w.WriteTo(ctx, out, WithFullRewrite(), WithObjectStreams(), WithDedup())
		},
		"split": func(ctx context.Context, r *Reader, out *bytes.Buffer) error {
			return Split(ctx, r, []int{2, 1}, out, WithBookmarks(BookmarkOptions{Handling: BookmarkReconstruct}))
		},
		"merge": func(ctx context.Context, r *Reader, out *bytes.Buffer) error {
			return Merge(ctx, []*Reader{r, r}, out, WithBookmarks(BookmarkOptions{Handling: BookmarkMixed, WrapEachSource: true}))
		},
	}
	for _, fixture := range []string{"outlinedest_ua2.pdf", "multipage_2b.pdf", "structcopy_ua2.pdf", "pagelabels_2b.pdf"} {
		for name, edit := range edits {
			t.Run(fixture+"/"+name, func(t *testing.T) {
				t.Parallel()
				data := readLithoFixture(t, fixture)
				sameOutputEveryRun(t, func() ([]byte, error) {
					ctx := context.Background()
					r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
					if err != nil {
						return nil, fmt.Errorf("opening: %w", err)
					}
					defer r.Close()
					var out bytes.Buffer
					if err := edit(ctx, r, &out); err != nil {
						return nil, err
					}
					return out.Bytes(), nil
				})
			})
		}
	}
}

// writeEdited applies edit to a Writer over r and writes the result to out.
func writeEdited(ctx context.Context, r *Reader, out *bytes.Buffer, edit func(*Writer) error) error {
	w := NewWriter(r)
	defer w.Close()
	if err := edit(w); err != nil {
		return fmt.Errorf("editing: %w", err)
	}
	if err := w.WriteTo(ctx, out); err != nil {
		return fmt.Errorf("writing: %w", err)
	}
	return nil
}
