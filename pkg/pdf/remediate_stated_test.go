package pdf

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// statedCase is one way a document comes to state, or not, what a PDF/UA
// remediation cannot know about it: its title and its language.
type statedCase struct {
	// head is what the source's html element and head state.
	head string
	// title is the title the caller sets before the remediation, "" for
	// none.
	title string
	// remediate are the remediation's options: what else the caller states.
	remediate []RemediateOption
	// wantTitle and wantLang are the output's /Title and catalog /Lang, ""
	// for none.
	wantTitle, wantLang string
	// missing names, per part, the clauses the residual must list and the
	// validator find, none when the output must conform.
	missing map[models.PDFUAProfile][]string
}

// remediateStated converts a one-line HTML page with tc's head, remediates
// it to profile as tc's caller does, and checks the residual, the
// validator's verdict and the output's title and language against tc.
func remediateStated(t *testing.T, profile models.PDFUAProfile, version string, tc statedCase) {
	t.Helper()
	ctx := context.Background()
	src := `<!doctype html>` + tc.head + `<p>A line of text.</p>`
	c := FromHTML(strings.NewReader(src), WithConvertFontDirs(convertFontDir(t)), WithConvertVersion(version))
	var rep *models.RemediationReport
	var out bytes.Buffer
	_, err := NewPipeline().Convert(c).Apply(func(ctx context.Context, w *Writer) error {
		if tc.title != "" {
			w.SetMetadata(models.Metadata{Title: tc.title})
		}
		var err error
		rep, err = w.RemediatePDFUA(ctx, profile, tc.remediate...)
		return err
	}).WriteTo(ctx, &out, WithFullRewrite(), WithVersion(version))
	if err != nil {
		t.Fatalf("converting and remediating: %v", err)
	}
	missing := tc.missing[profile]
	clauses := func(violations []models.Violation) []string {
		var got []string
		for _, v := range violations {
			got = append(got, v.Clause)
		}
		return got
	}
	if got := clauses(rep.ResidualViolations()); !sameClauses(got, missing) {
		t.Errorf("Residual = %v, want clauses %v", rep.Residual, missing)
	}
	r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close() //nolint:errcheck // test reader
	violations, err := r.ValidatePDFUAReport(ctx, profile)
	if err != nil {
		t.Fatalf("ValidatePDFUAReport: %v", err)
	}
	if got := clauses(violations); !sameClauses(got, missing) {
		t.Errorf("the validator finds %v, want clauses %v", violations, missing)
	}
	if got := catalogLang(t, r); got != tc.wantLang {
		t.Errorf("catalog /Lang = %q, want %q", got, tc.wantLang)
	}
	// PDF 2.0 deprecates the /Title (ISO 32000-2, 14.3.3), and the
	// validator's verdict above judges the dc:title.
	if got := infoTitle(t, r); version != "2.0" && got != tc.wantTitle {
		t.Errorf("/Title = %q, want %q", got, tc.wantTitle)
	}
}

// infoTitle returns the /Title of r's document information dictionary, ""
// for none. [Reader.Metadata] is no help here: its scan of older Info
// dictionaries reads an outline entry's /Title as one.
func infoTitle(t *testing.T, r *Reader) string {
	t.Helper()
	ctx := context.Background()
	info, err := document.ResolveDict(ctx, r.doc, r.doc.TrailerAsWritten()["Info"])
	if err != nil || info == nil {
		return ""
	}
	s, err := document.ResolveString(ctx, r.doc, info["Title"])
	if err != nil {
		return ""
	}
	return document.DecodePdfString(s)
}

// sameClauses reports whether got names each clause of want and no other.
func sameClauses(got, want []string) bool {
	seen := map[string]bool{}
	for _, c := range got {
		if !slices.Contains(want, c) {
			return false
		}
		seen[c] = true
	}
	return len(seen) == len(want)
}

// TestRemediatePDFUA_WritesNoLanguageNobodyStated checks that a PDF/UA
// remediation labels a document with the language its source or its
// caller states, and with none other: a document that states none is
// reported under ISO 14289-1, 7.2 (ISO 14289-2, 8.4.4), not labelled "en".
func TestRemediatePDFUA_WritesNoLanguageNobodyStated(t *testing.T) {
	for name, tc := range map[string]statedCase{
		"nobody": {
			head:      `<title>Notes</title>`,
			wantTitle: "Notes",
			missing:   map[models.PDFUAProfile][]string{models.PDFUA1: {"7.2"}, models.PDFUA2: {"8.4.4"}},
		},
		"caller": {
			head:      `<title>Notes</title>`,
			remediate: []RemediateOption{WithLanguage("fr-FR")},
			wantTitle: "Notes", wantLang: "fr-FR",
		},
		"source": {head: `<html lang="de"><title>Notes</title>`, wantTitle: "Notes", wantLang: "de"},
		"source over caller": {
			head:      `<html lang="de"><title>Notes</title>`,
			remediate: []RemediateOption{WithLanguage("fr-FR")},
			wantTitle: "Notes", wantLang: "de",
		},
	} {
		for profile, version := range map[models.PDFUAProfile]string{models.PDFUA1: "1.7", models.PDFUA2: "2.0"} {
			t.Run(name+"/"+string(profile), func(t *testing.T) {
				remediateStated(t, profile, version, tc)
			})
		}
	}
}

// TestRemediatePDFUA_WritesNoTitleNobodyStated checks that a PDF/UA
// remediation gives a document the title its source or its caller states,
// and none other: a document that states none is reported under ISO
// 14289-1, 7.1 (ISO 14289-2, 8.11.1), not titled from its first heading,
// its outline or a placeholder.
func TestRemediatePDFUA_WritesNoTitleNobodyStated(t *testing.T) {
	for name, tc := range map[string]statedCase{
		"nobody": {
			head:     `<html lang="en"><h1>A heading</h1>`,
			wantLang: "en",
			missing:  map[models.PDFUAProfile][]string{models.PDFUA1: {"7.1"}, models.PDFUA2: {"8.11.1"}},
		},
		"caller": {
			head:      `<html lang="en"><h1>A heading</h1>`,
			title:     "Meeting notes",
			wantTitle: "Meeting notes", wantLang: "en",
		},
		"source": {head: `<html lang="en"><title>Notes</title>`, wantTitle: "Notes", wantLang: "en"},
		"neither title nor language": {
			missing: map[models.PDFUAProfile][]string{models.PDFUA1: {"7.1", "7.2"}, models.PDFUA2: {"8.11.1", "8.4.4"}},
		},
	} {
		for profile, version := range map[models.PDFUAProfile]string{models.PDFUA1: "1.7", models.PDFUA2: "2.0"} {
			t.Run(name+"/"+string(profile), func(t *testing.T) {
				remediateStated(t, profile, version, tc)
			})
		}
	}
}
