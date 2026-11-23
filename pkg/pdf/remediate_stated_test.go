package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// statedCase is one way a document comes to state, or not, what a PDF/UA
// remediation cannot know about it.
type statedCase struct {
	// convert are the conversion's options: what the source states.
	convert []ConvertOption
	// remediate are the remediation's: what the caller states.
	remediate []RemediateOption
	// lang is the catalog /Lang the output must carry, "" for none.
	lang string
	// missing names the clause of each part the residual must list, nil
	// when the output must conform.
	missing map[models.PDFUAProfile]string
}

// remediateStated converts a plain text with tc's options, remediates it
// to profile with tc's, and checks the residual and the output's language
// against tc.
func remediateStated(t *testing.T, profile models.PDFUAProfile, version string, tc statedCase) {
	t.Helper()
	ctx := context.Background()
	opts := []TextOption{WithConvertFontDirs(convertFontDir(t)), WithConvertVersion(version)}
	for _, o := range tc.convert {
		opts = append(opts, o)
	}
	c := FromText(strings.NewReader("A line of text."), opts...)
	var rep *models.RemediationReport
	var out bytes.Buffer
	_, err := NewPipeline().Convert(c).Apply(func(ctx context.Context, w *Writer) error {
		var err error
		rep, err = w.RemediatePDFUA(ctx, profile, tc.remediate...)
		return err
	}).WriteTo(ctx, &out, WithFullRewrite(), WithVersion(version))
	if err != nil {
		t.Fatalf("converting and remediating: %v", err)
	}
	clause := tc.missing[profile]
	listed := false
	for _, v := range rep.ResidualViolations() {
		if clause != "" && v.Clause == clause {
			listed = true
			continue
		}
		t.Errorf("Residual lists %v", v)
	}
	if clause != "" && !listed {
		t.Errorf("Residual = %v, want clause %s", rep.Residual, clause)
	}
	r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close() //nolint:errcheck // test reader
	if got := catalogLang(t, r); got != tc.lang {
		t.Errorf("catalog /Lang = %q, want %q", got, tc.lang)
	}
	violations, err := r.ValidatePDFUAReport(ctx, profile)
	if err != nil {
		t.Fatalf("ValidatePDFUAReport: %v", err)
	}
	if conforms := len(violations) == 0; conforms != (clause == "") {
		t.Errorf("the validator finds %v", violations)
	}
}

// TestRemediatePDFUA_WritesNoLanguageNobodyStated checks that a PDF/UA
// remediation labels a document with the language its source or its
// caller states, and with none other: a document that states none is
// reported under ISO 14289-1, 7.2 (ISO 14289-2, 8.4.4), not labelled "en".
func TestRemediatePDFUA_WritesNoLanguageNobodyStated(t *testing.T) {
	for name, tc := range map[string]statedCase{
		"nobody": {
			missing: map[models.PDFUAProfile]string{models.PDFUA1: "7.2", models.PDFUA2: "8.4.4"},
		},
		"caller": {remediate: []RemediateOption{WithLanguage("fr-FR")}, lang: "fr-FR"},
		"source": {convert: []ConvertOption{WithDefaultLanguage("de")}, lang: "de"},
		"source over caller": {
			convert:   []ConvertOption{WithDefaultLanguage("de")},
			remediate: []RemediateOption{WithLanguage("fr-FR")},
			lang:      "de",
		},
	} {
		for profile, version := range map[models.PDFUAProfile]string{models.PDFUA1: "1.7", models.PDFUA2: "2.0"} {
			t.Run(name+"/"+string(profile), func(t *testing.T) {
				remediateStated(t, profile, version, tc)
			})
		}
	}
}
