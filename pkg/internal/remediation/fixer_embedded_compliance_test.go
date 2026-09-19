package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// twoEmbeddedFilePDF carries two embedded file specifications, object 4 and
// object 7. Two are needed because the repair is violation-targeted: a fixer
// that stripped every /EF would pass a single-attachment test while destroying
// the conforming attachments parts 2 and 4 explicitly permit.
func twoEmbeddedFilePDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 5 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 6 0 R >> >>",
		"<< /Names [(a.txt) 4 0 R (b.pdf) 7 0 R] >>",
		"<< /Length 3 >>\nstream\nabc\nendstream",
		"<< /Type /Filespec /F (b.pdf) /UF (b.pdf) /EF << /F 8 0 R >> >>",
		"<< /Length 3 >>\nstream\nxyz\nendstream",
	)
}

func runEmbeddedCompliance(t *testing.T, preserve bool, rule compliance.Rule, contexts ...string) (*writer.ObjAllocator, []string) {
	t.Helper()

	doc, alloc := newTestDoc(t, twoEmbeddedFilePDF())
	violations := make([]compliance.Violation, 0, len(contexts))
	for _, c := range contexts {
		violations = append(violations, compliance.Violation{Rule: rule, Context: c})
	}
	fixes, err := fixEmbeddedCompliance(context.Background(), alloc, doc,
		Options{PreserveContent: preserve}, violations, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixEmbeddedCompliance: %v", err)
	}
	return alloc, fixes
}

// TestFixEmbeddedCompliance_StripsOnlyTheNamedSpec pins the repair of
// ISO 19005-2 6.8 t5: the attachment the violation names loses /EF, and the
// other one, which the check did not flag, keeps it.
func TestFixEmbeddedCompliance_StripsOnlyTheNamedSpec(t *testing.T) {
	t.Parallel()

	alloc, fixes := runEmbeddedCompliance(t, false, compliance.Rule_19005_2_6_8_t05,
		"object 4: embedded file is not a PDF document")

	spec, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok {
		t.Fatalf("the flagged file specification was not rewritten; fixes=%v", fixes)
	}
	if _, present := spec["EF"]; present {
		t.Error("/EF survived on the flagged file specification")
	}
	// The rule forbids the embedded file, not the specification: stripping more
	// would discard the reference to the external file as well.
	if _, present := spec["F"]; !present {
		t.Error("the repair removed more than /EF")
	}
	if _, present := alloc.Pending()[7]; present {
		t.Error("an unflagged embedded file specification was rewritten")
	}
	if len(fixes) != 1 {
		t.Errorf("fixes = %v, want exactly one", fixes)
	}
}

// TestFixEmbeddedCompliance_RepairsThePDFA4Rule covers the other claimed rule.
// ISO 19005-4 6.9 t3 is the part-4 embedded-file requirement, and it fires only
// for bare PDF/A-4, never for 4e or 4f.
func TestFixEmbeddedCompliance_RepairsThePDFA4Rule(t *testing.T) {
	t.Parallel()

	alloc, _ := runEmbeddedCompliance(t, false, compliance.Rule_19005_4_6_9_t03,
		"object 7: embedded file is not compliant with an accepted PDF/A part")

	spec, ok := alloc.Pending()[7].(ast.PdfDict)
	if !ok {
		t.Fatal("the flagged file specification was not rewritten")
	}
	if _, present := spec["EF"]; present {
		t.Error("/EF survived on the flagged file specification")
	}
}

// TestFixEmbeddedCompliance_LeavesTheNameTreeAlone guards against introducing a
// violation while removing one. PDF/A-4f requires /Names to carry an
// /EmbeddedFiles entry, so deleting the tree to tidy up would trade this
// violation for that one, a swap a count-based gate cannot see.
func TestFixEmbeddedCompliance_LeavesTheNameTreeAlone(t *testing.T) {
	t.Parallel()

	alloc, _ := runEmbeddedCompliance(t, false, compliance.Rule_19005_2_6_8_t05,
		"object 4: embedded file is not a PDF document")

	if _, present := alloc.Pending()[1]; present {
		t.Error("the catalog was rewritten; the repair must not touch /Names")
	}
	if _, present := alloc.Pending()[5]; present {
		t.Error("the EmbeddedFiles name tree was rewritten")
	}
}

// TestFixEmbeddedCompliance_PreserveContentRefuses is the first negative
// control: dropping an attachment destroys content the author put there.
func TestFixEmbeddedCompliance_PreserveContentRefuses(t *testing.T) {
	t.Parallel()

	alloc, fixes := runEmbeddedCompliance(t, true, compliance.Rule_19005_2_6_8_t05,
		"object 4: embedded file is not a PDF document")

	if len(alloc.Pending()) != 0 {
		t.Errorf("the repair wrote %d object(s) while preserving content", len(alloc.Pending()))
	}
	if len(fixes) != 0 {
		t.Errorf("fixes = %v, want none while preserving content", fixes)
	}
}

// TestFixEmbeddedCompliance_DeclinesAnUnreadableContext is the second negative
// control. Object 0 is the shape a direct file specification produces: it parses
// as a number but addresses nothing, so acting on it would write to a bogus
// object rather than decline.
func TestFixEmbeddedCompliance_DeclinesAnUnreadableContext(t *testing.T) {
	t.Parallel()

	for _, ctxStr := range []string{
		"",
		"object 0: embedded file is not a PDF document",
		"object -4: embedded file is not a PDF document",
		"object x: embedded file is not a PDF document",
		"object 4 embedded file is not a PDF document",
		"the embedded file is not a PDF document",
		"page 4: something else entirely",
	} {
		alloc, fixes := runEmbeddedCompliance(t, false, compliance.Rule_19005_2_6_8_t05, ctxStr)
		if len(alloc.Pending()) != 0 {
			t.Errorf("context %q: wrote %d object(s), want none", ctxStr, len(alloc.Pending()))
		}
		if len(fixes) != 0 {
			t.Errorf("context %q: fixes = %v, want none", ctxStr, fixes)
		}
	}
}

// TestMatchEmbeddedCompliance_ClaimsExactlyTwoRules is the mirror test. The
// negatives matter more than the positives here: clause 6.9 means something
// different in every part, and ISO 19005-2 6.9 t3 shares the clause and test
// number of the part-4 rule while being an optional-content requirement.
func TestMatchEmbeddedCompliance_ClaimsExactlyTwoRules(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_2_6_8_t05,
		compliance.Rule_19005_4_6_9_t03,
	} {
		if !matchEmbeddedCompliance(compliance.Violation{Rule: rule}) {
			t.Errorf("must match embedded-compliance rule %s/%s t%d",
				rule.Specification, rule.Clause, rule.TestNumber)
		}
	}

	for _, rule := range []compliance.Rule{
		{Specification: "ISO_19005_2", Clause: "6.9", TestNumber: 3}, // optional content, not embedded files
		{Specification: "ISO_19005_3", Clause: "6.9", TestNumber: 3},
		{Specification: "ISO_19005_1", Clause: "6.9", TestNumber: 3}, // interactive forms
		{Specification: "ISO_19005_4", Clause: "6.8", TestNumber: 5},
		{Specification: "ISO_19005_2", Clause: "6.8", TestNumber: 4}, // /AFRelationship
		{Specification: "ISO_19005_3", Clause: "6.8", TestNumber: 5},
		{Specification: "ISO_19005_4", Clause: "6.9", TestNumber: 4},
	} {
		if matchEmbeddedCompliance(compliance.Violation{Rule: rule}) {
			t.Errorf("must not match %s/%s t%d",
				rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}

func TestParseEmbeddedObjectContext(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in     string
		want   int
		wantOK bool
	}{
		{"object 6: embedded file is not a PDF document", 6, true},
		{"object 12: embedded file is not a valid PDF", 12, true},
		{"object 41: embedded file is not compliant with an accepted PDF/A part", 41, true},
		{"object 0: embedded file is not a PDF document", 0, false},
		{"object -1: embedded file is not a PDF document", 0, false},
		{"object : embedded file", 0, false},
		{"object 6 /Names/EmbeddedFiles", 0, false},
		{"objects 6: embedded file", 0, false},
		{"", 0, false},
	} {
		got, ok := parseEmbeddedObjectContext(tc.in)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("parseEmbeddedObjectContext(%q) = (%d, %v), want (%d, %v)",
				tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}
