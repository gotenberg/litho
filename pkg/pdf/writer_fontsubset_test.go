package pdf

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// fontSubsetFixtures are litho corpus fixtures embedding whole TrueType
// programs the document shows a few glyphs of, simple and composite.
var fontSubsetFixtures = []string{"glyphs_2b.pdf", "ttcmap_2b.pdf", "charset_1b.pdf", "fontleft_2b.pdf"}

// ruleCounts returns the rules a validation at profile reports, with how
// often each fires, in order.
func ruleCounts(t *testing.T, r *Reader, profile models.PDFAProfile) []string {
	t.Helper()
	vs, err := r.ValidatePDFAReport(context.Background(), profile)
	if err != nil {
		t.Fatalf("ValidatePDFAReport(%s): %v", profile, err)
	}
	counts := make(map[string]int)
	for _, v := range vs {
		counts[fmt.Sprintf("%s t%d", v.Clause, v.TestNumber)]++
	}
	out := make([]string, 0, len(counts))
	for rule, n := range counts {
		out = append(out, fmt.Sprintf("%s x%d", rule, n))
	}
	slices.Sort(out)
	return out
}

// TestWriter_WithFontSubsetting_IsLossless is the end-to-end contract: the
// option shrinks the output, the text extracts and the PDF/A verdicts read as
// they did, and two runs give the same bytes.
func TestWriter_WithFontSubsetting_IsLossless(t *testing.T) {
	t.Parallel()
	for _, name := range fontSubsetFixtures {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			data, err := os.ReadFile(filepath.Join(lithoFixtureDir, name))
			if err != nil {
				t.Fatalf("reading the fixture: %v", err)
			}
			write := func(opts ...WriteOption) []byte {
				t.Helper()
				var out bytes.Buffer
				if err := NewWriter(openRecompressSource(t, data)).WriteTo(ctx, &out, append([]WriteOption{WithFullRewrite()}, opts...)...); err != nil {
					t.Fatalf("WriteTo: %v", err)
				}
				return out.Bytes()
			}
			plain := write()
			subset := write(WithFontSubsetting())
			if len(subset) >= len(plain) {
				t.Fatalf("subset output is %d bytes against %d without the option", len(subset), len(plain))
			}
			if !bytes.Equal(subset, write(WithFontSubsetting())) {
				t.Fatal("two runs of the same write differ")
			}
			before, after := openRecompressSource(t, plain), openRecompressSource(t, subset)
			wantText, errBefore := before.ExtractAllText(ctx)
			gotText, errAfter := after.ExtractAllText(ctx)
			if gotText != wantText || (errBefore == nil) != (errAfter == nil) {
				t.Errorf("text %q (%v), want %q (%v)", gotText, errAfter, wantText, errBefore)
			}
			for _, profile := range []models.PDFAProfile{models.PDFA1b, models.PDFA2b} {
				if got, want := ruleCounts(t, after, profile), ruleCounts(t, before, profile); !slices.Equal(got, want) {
					t.Errorf("%s rules %v, want %v", profile, got, want)
				}
			}
		})
	}
}

// TestWriter_WithFontSubsetting_ReportsIncrementalUpdate checks that a write
// appending an incremental update subsets nothing and says so.
func TestWriter_WithFontSubsetting_ReportsIncrementalUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, err := os.ReadFile(filepath.Join(lithoFixtureDir, fontSubsetFixtures[0]))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	write := func(opts ...WriteOption) []byte {
		t.Helper()
		w := NewWriter(openRecompressSource(t, data))
		if err := w.RotatePages(90, 1); err != nil {
			t.Fatalf("RotatePages: %v", err)
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out, opts...); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return out.Bytes()
	}
	var skipped bool
	asked := write(WithFontSubsetting(), WithCompactionSkipped(&skipped))
	if !skipped {
		t.Error("an incremental update did not report the skipped subsetting")
	}
	if !bytes.Equal(asked, write()) {
		t.Error("an incremental update changed with the option")
	}
}

// TestWriter_WithFontSubsetting_AppliesToEncryptedOutput checks that the
// option, which runs before encryption, still subsets an encrypted output.
func TestWriter_WithFontSubsetting_AppliesToEncryptedOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, err := os.ReadFile(filepath.Join(lithoFixtureDir, fontSubsetFixtures[0]))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	write := func(opts ...WriteOption) []byte {
		t.Helper()
		var out bytes.Buffer
		opts = append(opts, WithEncryption(EncryptConfig{UserPassword: "u", OwnerPassword: "o"}))
		if err := NewWriter(openRecompressSource(t, data)).WriteTo(ctx, &out, opts...); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return out.Bytes()
	}
	plain, subset := write(), write(WithFontSubsetting())
	if len(subset) >= len(plain) {
		t.Fatalf("encrypted output is %d bytes with the option and %d without", len(subset), len(plain))
	}
	want, err := openRecompressSource(t, plain, WithPassword("u")).ExtractAllText(ctx)
	if err != nil {
		t.Fatalf("ExtractAllText on the plain output: %v", err)
	}
	got, err := openRecompressSource(t, subset, WithPassword("u")).ExtractAllText(ctx)
	if err != nil || got != want {
		t.Errorf("text %q (%v), want %q", got, err, want)
	}
}
