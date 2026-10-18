package compliance

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestCheckOCGOrderCompleteness_IndirectOrder pins that an /Order array written
// as an indirect reference is walked rather than counted as an OCG.
//
// ISO 32000-1, 7.3.10 lets any value be an indirect reference, and Table 101
// makes /Order an array; its nested groupings are arrays too. Reading the
// reference itself as an OCG recorded the array's own object number and
// descended no further, so every OCG the file does list looked absent and
// ISO 19005-2 §6.9 t3 fired.
//
// # Corpus evidence
//
//   - pdfjs cmykjpeg.pdf, issue11144_reduced.pdf, transparent.pdf: all write
//     /Order as "N 0 R", and veraPDF reports none of them.
func TestCheckOCGOrderCompleteness_IndirectOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// fires reports whether the Order-completeness rule fails for a catalog
	// whose /D config carries the given /Order entry. Objects 4 and 5 are the
	// two OCGs; object 6 is an array holding both.
	fires := func(t *testing.T, order string) []Violation {
		t.Helper()
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [4 0 R 5 0 R] "+
				"/D << /Order "+order+" >> >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /OCG /Name (Layer 1) >>",
			"<< /Type /OCG /Name (Layer 2) >>",
			"[4 0 R 5 0 R]",
		)
		catalog, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 1})
		if err != nil {
			t.Fatalf("resolve catalog: %v", err)
		}
		return checkOCGOrderCompleteness(ctx, doc, catalog, 2)
	}

	cases := []struct {
		name  string
		order string
		want  bool
	}{
		{"an indirect /Order array is walked", "6 0 R", false},
		{"a direct /Order array still is", "[4 0 R 5 0 R]", false},
		{"a nested grouping is walked", "[(Group) [4 0 R 5 0 R]]", false},
		{"an OCG left out of /Order still fires", "[4 0 R]", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := fires(t, tc.order)
			if (len(got) > 0) != tc.want {
				t.Errorf("Order-completeness violation = %v, want %v (%v)", len(got) > 0, tc.want, got)
			}
		})
	}
}

// TestCheckOCGOrderCompleteness_NamesTheLowestMissingOCG pins that a file
// omitting several OCGs always names the same one. Ranging the OCG map let the
// iteration order decide, so the reported object number changed between runs on
// identical input.
func TestCheckOCGOrderCompleteness_NamesTheLowestMissingOCG(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [5 0 R 4 0 R] "+
			"/D << /Order [] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /OCG /Name (Layer 1) >>",
		"<< /Type /OCG /Name (Layer 2) >>",
	)
	catalog, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 1})
	if err != nil {
		t.Fatalf("resolve catalog: %v", err)
	}
	for range 12 {
		got := checkOCGOrderCompleteness(ctx, doc, catalog, 2)
		if len(got) != 1 {
			t.Fatalf("violations = %d, want 1", len(got))
		}
		if msg := got[0].Context; msg != "default config (D): OCG object 4 not referenced in Order array" {
			t.Fatalf("message = %q, want the lowest missing OCG object number", msg)
		}
	}
}

// ocgOrderViolations reports the Order-completeness verdict for a catalog whose
// /D config carries the given /Order entry, with objects 4 and 5 as the OCGs.
func ocgOrderViolations(t *testing.T, ctx context.Context, order string, extra ...string) []Violation {
	t.Helper()

	objs := append([]string{
		"<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [4 0 R 5 0 R] " +
			"/D << /Order " + order + " >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /OCG /Name (Layer 1) >>",
		"<< /Type /OCG /Name (Layer 2) >>",
	}, extra...)

	doc, _ := objsDocPDF(t, objs...)
	catalog, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 1})
	if err != nil {
		t.Fatalf("resolve catalog: %v", err)
	}
	return checkOCGOrderCompleteness(ctx, doc, catalog, 2)
}

// TestCheckOCGOrderCompleteness_DeepNestingDoesNotFabricate pins that nesting
// /Order past [maxOCGOrderDepth] does not fail a file that lists every OCG.
//
// The bound stopped the collection and the completeness loop then read every
// OCG it had not reached as unreferenced. Measured on a file naming both OCGs
// at every level: 20 levels reported nothing, 21 reported "OCG object 4 not
// referenced in Order array", and so did 40. ISO 19005-2 §6.9 t3 asks what the
// file contains, so an anti-DoS cap must not answer it.
func TestCheckOCGOrderCompleteness_DeepNestingDoesNotFabricate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Either side of the bound, and far past it.
	nestings := []int{maxOCGOrderDepth, maxOCGOrderDepth + 1, maxOCGOrderDepth * 3}
	for _, nest := range nestings {
		order := strings.Repeat("[", nest) + "4 0 R 5 0 R" + strings.Repeat("]", nest)
		if got := ocgOrderViolations(t, ctx, order); len(got) > 0 {
			t.Errorf("/Order nested %d levels: violations = %d, want 0, both OCGs are listed (%v)",
				nest, len(got), got)
		}
	}
}

// TestCheckOCGOrderCompleteness_CyclicOrderStillJudged pins that a grouping
// naming itself is walked once rather than spending the depth budget on the
// cycle.
//
// This is what keeps abstaining on a truncated read from becoming a way to hide
// a real violation. Without the visited set the cycle exhausts the bound, the
// walk reports itself incomplete, and object 5, an OCG /Order genuinely never
// names, goes unreported.
func TestCheckOCGOrderCompleteness_CyclicOrderStillJudged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Object 6 is a grouping holding OCG 4 and itself.
	got := ocgOrderViolations(t, ctx, "6 0 R", "[4 0 R 6 0 R]")
	if len(got) != 1 {
		t.Fatalf("violations = %d, want 1: object 5 is an OCG /Order never names (%v)", len(got), got)
	}
	if msg := got[0].Context; msg != "default config (D): OCG object 5 not referenced in Order array" {
		t.Fatalf("message = %q, want object 5 named", msg)
	}
}

// ocPropsViolations reports the Order-completeness verdict at the given part
// for a catalog whose /OCProperties is the given dictionary, with objects 4
// and 5 as the OCGs.
func ocPropsViolations(t *testing.T, ctx context.Context, part int, ocProps string, extra ...string) []Violation {
	t.Helper()

	objs := append([]string{
		"<< /Type /Catalog /Pages 2 0 R /OCProperties " + ocProps + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /OCG /Name (Layer 1) >>",
		"<< /Type /OCG /Name (Layer 2) >>",
	}, extra...)

	doc, _ := objsDocPDF(t, objs...)
	catalog, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 1})
	if err != nil {
		t.Fatalf("resolve catalog: %v", err)
	}
	return checkOCGOrderCompleteness(ctx, doc, catalog, part)
}

// TestCheckOCGOrderCompleteness_EveryConfiguration pins that /Order is judged
// in every optional content configuration dictionary, /Configs included.
//
// ISO 19005-2 §6.9 t3 and ISO 19005-4 §6.10 t3 state the rule of "an optional
// content configuration dictionary", and veraPDF tests it on every PDOCConfig:
// the default /D and each /Configs entry. Reading /D alone missed a /Configs
// configuration whose own /Order leaves an OCG out.
//
// # Corpus evidence
//
//   - litho complex_ua2.pdf: /Configs [ << /Order [ ] >> ], an incomplete
//     /Order litho never inspected while /D also failed.
func TestCheckOCGOrderCompleteness_EveryConfiguration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name    string
		ocProps string
		extra   []string
		want    []string
	}{
		{
			name:    "a /Configs entry missing an OCG fires while /D passes",
			ocProps: "<< /OCGs [4 0 R 5 0 R] /D << /Order [4 0 R 5 0 R] >> /Configs [ << /Order [4 0 R] >> ] >>",
			want:    []string{"config 0: OCG object 5 not referenced in Order array"},
		},
		{
			name:    "an indirect /Configs entry is judged",
			ocProps: "<< /OCGs [4 0 R 5 0 R] /D << /Name (D) >> /Configs [6 0 R] >>",
			extra:   []string{"<< /Name (Alt) /Order [] >>"},
			want:    []string{"config 0: OCG object 4 not referenced in Order array"},
		},
		{
			name:    "each failing configuration is reported once",
			ocProps: "<< /OCGs [4 0 R 5 0 R] /D << /Order [5 0 R] >> /Configs [ << /Order [4 0 R 5 0 R] >> << /Order [] >> ] >>",
			want: []string{
				"default config (D): OCG object 4 not referenced in Order array",
				"config 1: OCG object 4 not referenced in Order array",
			},
		},
		{
			name:    "a configuration without /Order passes",
			ocProps: "<< /OCGs [4 0 R 5 0 R] /D << /Name (D) >> /Configs [ << /Name (Alt) >> ] >>",
		},
		{
			name:    "a /Configs entry that is not a dictionary is ignored",
			ocProps: "<< /OCGs [4 0 R 5 0 R] /D << /Order [4 0 R 5 0 R] >> /Configs [ (junk) 7 ] >>",
		},
		{
			// veraPDF ignores an /Order that is not an array ("Invalid object
			// type of Order entry"): the clause judges the contents of an
			// Order array, not its type.
			name:    "an /Order that is not an array is not judged",
			ocProps: "<< /OCGs [4 0 R 5 0 R] /D << /Order 4 0 R >> /Configs [ << /Order /Bad >> ] >>",
		},
		{
			// veraPDF validates an indirect object once however many paths
			// reach it.
			name:    "a configuration reached twice is reported once",
			ocProps: "<< /OCGs [4 0 R 5 0 R] /D 6 0 R /Configs [6 0 R] >>",
			extra:   []string{"<< /Name (D) /Order [4 0 R] >>"},
			want:    []string{"default config (D): OCG object 5 not referenced in Order array"},
		},
	}
	for _, part := range []int{2, 3, 4} {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				got := ocPropsViolations(t, ctx, part, tc.ocProps, tc.extra...)
				var msgs []string
				for _, v := range got {
					if v.Rule != ocgOrderCompletePDFARule(part) {
						t.Errorf("part %d: rule = %v, want %v", part, v.Rule, ocgOrderCompletePDFARule(part))
					}
					msgs = append(msgs, v.Context)
				}
				if !slices.Equal(msgs, tc.want) {
					t.Errorf("part %d: violations = %q, want %q", part, msgs, tc.want)
				}
			})
		}
	}
}
