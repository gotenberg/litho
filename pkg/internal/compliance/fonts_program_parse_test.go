package compliance

import (
	"bytes"
	"context"
	"errors"
	"os"
	"regexp"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// fixtureFontsFailing validates the litho corpus fixture name at profile and
// returns, sorted, the resource names of the fonts each violation of rule
// names, one entry per violation.
func fixtureFontsFailing(t *testing.T, name, profile string, rule Rule) []string {
	t.Helper()
	ctx := context.Background()
	data, err := os.ReadFile("../../../test/integration/corpus/litho/pdfs/" + name)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	var viols []error
	if IsPDFUA(profile) {
		viols, err = ValidatePDFUA(ctx, doc, models.PDFUAProfile(profile))
	} else {
		viols, err = ValidatePDFA(ctx, doc, models.PDFAProfile(profile))
	}
	if err != nil {
		t.Fatalf("validating at %s: %v", profile, err)
	}
	fontRe := regexp.MustCompile(`font (\S+) on page`)
	var fonts []string
	for _, e := range viols {
		var v Violation
		if !errors.As(e, &v) || v.Rule.Specification != rule.Specification || v.Rule.Clause != rule.Clause || v.Rule.TestNumber != rule.TestNumber {
			continue
		}
		m := fontRe.FindStringSubmatch(v.Context)
		if m == nil {
			fonts = append(fonts, "?")
			continue
		}
		fonts = append(fonts, m[1])
	}
	slices.Sort(fonts)
	return fonts
}

// TestFontProgramParseDecidesEmbedding pins that a font program in the right
// container that veraPDF's parser cannot finish embeds nothing, under ISO
// 19005-1, 6.3.4, ISO 19005-2, 6.2.11.4.1 t1, ISO 19005-4, 6.2.10.4.1 t1, ISO
// 14289-1, 7.21.4.1 t1 and ISO 14289-2, 8.4.5.5.1 t1, and that no cmap rule
// then weighs it. The litho fixture fontunread_2b.pdf plants U1 to U4, T1 to T3
// and O1, the programs [fontProgramReadable] rejects, beside the embedded
// controls OK, CN and TK. veraPDF 1.30.2 reports exactly the eight in all
// thirteen cells.
func TestFontProgramParseDecidesEmbedding(t *testing.T) {
	t.Parallel()
	unread := []string{"O1", "T1", "T2", "T3", "U1", "U2", "U3", "U4"}
	for _, tt := range []struct {
		profile string
		rule    Rule
	}{
		{"PDF/A-1b", Rule_19005_1_6_3_4_t01},
		{"PDF/A-2b", Rule_19005_2_6_2_11_4_1_t01},
		{"PDF/A-3u", Rule_19005_3_6_2_11_4_1_t01},
		{"PDF/A-4", Rule_19005_4_6_2_10_4_1_t01},
		{"PDF/UA-1", Rule_14289_1_7_21_4_1_t01},
		{"PDF/UA-2", Rule_14289_2_8_4_5_5_1_t01},
	} {
		t.Run(tt.profile, func(t *testing.T) {
			t.Parallel()
			if got := fixtureFontsFailing(t, "fontunread_2b.pdf", tt.profile, tt.rule); !slices.Equal(got, unread) {
				t.Errorf("fonts not embedded = %v, want %v", got, unread)
			}
		})
	}
	t.Run("no cmap rule on a program that does not parse", func(t *testing.T) {
		t.Parallel()
		if got := fixtureFontsFailing(t, "fontunread_2b.pdf", "PDF/A-2b", Rule_19005_2_6_2_11_6_t01); len(got) != 0 {
			t.Errorf("6.2.11.6 t1 fired for %v, want none", got)
		}
	})
}
