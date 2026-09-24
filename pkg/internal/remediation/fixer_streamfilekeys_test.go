package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// streamFileKeysPDF builds a document whose object 4 is a Form XObject carrying
// all three forbidden keys, and object 6 a second stream carrying only /F. Two
// are needed because the repair is violation-targeted: a fixer that swept every
// stream would pass a single-stream test while rewriting objects no violation
// named.
//
// /Length 0 mirrors real carriers: the bytes were external, so the embedded
// stream is empty.
//
// # Corpus evidence
//
//   - All corpora: the fixtures carrying these keys declare /Length 0.
func streamFileKeysPDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Length 0 /Type /XObject /Subtype /Form /BBox [0 0 10 10] /F 5 0 R /FFilter /FlateDecode /FDecodeParms << /Predictor 1 >> >>\nstream\n\nendstream",
		"<< /Type /Filespec /F (external.bin) >>",
		"<< /Length 0 /Type /XObject /Subtype /Form /BBox [0 0 10 10] /F 5 0 R >>\nstream\n\nendstream",
	)
}

func runStreamFileKeys(t *testing.T, preserve bool, contexts ...string) (*writer.ObjAllocator, []string) {
	t.Helper()

	doc, alloc := newTestDoc(t, streamFileKeysPDF())
	violations := make([]compliance.Violation, 0, len(contexts))
	for _, c := range contexts {
		violations = append(violations, compliance.Violation{
			Rule:    compliance.Rule{Specification: "ISO_19005_2", Clause: "6.1.7.1", TestNumber: 3},
			Context: c,
		})
	}
	fixes, err := fixStreamFileKeys(context.Background(), alloc, doc,
		Options{PreserveContent: preserve}, violations, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixStreamFileKeys: %v", err)
	}
	return alloc, fixes
}

// TestFixStreamFileKeys_StripsOnlyTheNamedKeys pins the repair: the three
// forbidden keys go, every other entry stays, and the stream body survives.
func TestFixStreamFileKeys_StripsOnlyTheNamedKeys(t *testing.T) {
	t.Parallel()

	alloc, fixes := runStreamFileKeys(t, false,
		"object 4: stream dict contains [F FFilter FDecodeParms]")

	s, ok := alloc.Pending()[4].(ast.PdfStream)
	if !ok {
		t.Fatalf("object 4 was not rewritten as a stream; fixes=%v", fixes)
	}
	for _, k := range []ast.PdfName{"F", "FFilter", "FDecodeParms"} {
		if _, present := s.Dict[k]; present {
			t.Errorf("/%s survived on the stream dictionary", k)
		}
	}
	// The rule forbids these three keys, nothing else.
	for _, k := range []ast.PdfName{"Type", "Subtype", "BBox"} {
		if _, present := s.Dict[k]; !present {
			t.Errorf("the repair removed more than the forbidden keys: /%s is gone", k)
		}
	}
	// An empty body is the normal case: /Length 0 because the data was external.
	raw, err := s.Bytes(context.Background())
	if err != nil {
		t.Fatalf("reading the rewritten body: %v", err)
	}
	if len(raw) != 0 {
		t.Errorf("body = %d bytes, want 0 (the fixture declares /Length 0)", len(raw))
	}
	if len(fixes) != 1 {
		t.Errorf("fixes = %v, want exactly one", fixes)
	}
}

// TestFixStreamFileKeys_LeavesUnnamedObjectsAlone is the targeting control.
// Object 6 also carries /F, but no violation named it.
func TestFixStreamFileKeys_LeavesUnnamedObjectsAlone(t *testing.T) {
	t.Parallel()

	alloc, _ := runStreamFileKeys(t, false,
		"object 4: stream dict contains [F FFilter FDecodeParms]")

	if _, present := alloc.Pending()[6]; present {
		t.Error("an unnamed stream was rewritten; the repair must be violation-targeted")
	}
}

// TestFixStreamFileKeys_RemovesOnlyTheKeysTheDetectorFound covers the partial
// case: a stream carrying just /F must lose only /F.
func TestFixStreamFileKeys_RemovesOnlyTheKeysTheDetectorFound(t *testing.T) {
	t.Parallel()

	alloc, _ := runStreamFileKeys(t, false, "object 6: stream dict contains [F]")

	s, ok := alloc.Pending()[6].(ast.PdfStream)
	if !ok {
		t.Fatal("object 6 was not rewritten")
	}
	if _, present := s.Dict["F"]; present {
		t.Error("/F survived")
	}
	if _, present := s.Dict["Subtype"]; !present {
		t.Error("the repair removed more than /F")
	}
}

// TestFixStreamFileKeys_PreserveContentRefuses is the first negative control.
func TestFixStreamFileKeys_PreserveContentRefuses(t *testing.T) {
	t.Parallel()

	alloc, fixes := runStreamFileKeys(t, true,
		"object 4: stream dict contains [F FFilter FDecodeParms]")

	if len(alloc.Pending()) != 0 {
		t.Errorf("wrote %d object(s) while preserving content", len(alloc.Pending()))
	}
	if len(fixes) != 0 {
		t.Errorf("fixes = %v, want none", fixes)
	}
}

// TestFixStreamFileKeys_DeclinesAnUnreadableContext is the second negative
// control. Object 0 addresses nothing, and a key outside the forbidden set must
// never be deleted just because a context names it.
func TestFixStreamFileKeys_DeclinesAnUnreadableContext(t *testing.T) {
	t.Parallel()

	for _, ctxStr := range []string{
		"",
		"object 0: stream dict contains [F]",
		"object -4: stream dict contains [F]",
		"object 4: stream dict contains []",
		"object 4: stream dict contains [Length]",
		"object 4: declared /Length=8 does not match the stream data extent",
		"object 4 stream dict contains [F]",
		"page 4: something else entirely",
	} {
		alloc, fixes := runStreamFileKeys(t, false, ctxStr)
		if len(alloc.Pending()) != 0 {
			t.Errorf("context %q: wrote %d object(s), want none", ctxStr, len(alloc.Pending()))
		}
		if len(fixes) != 0 {
			t.Errorf("context %q: fixes = %v, want none", ctxStr, fixes)
		}
	}
}

// TestMatchStreamFileKeys_ClaimsExactlyFourSpellings is the mirror test. The
// negatives carry the weight: ISO 19005-4 spells this rule 6.1.6.1 t2 while its
// clause 6.1.7 is CosUnicodeName, an unrelated requirement.
func TestMatchStreamFileKeys_ClaimsExactlyFourSpellings(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		{Specification: "ISO_19005_1", Clause: "6.1.7", TestNumber: 3},
		{Specification: "ISO_19005_2", Clause: "6.1.7.1", TestNumber: 3},
		{Specification: "ISO_19005_3", Clause: "6.1.7.1", TestNumber: 3},
		{Specification: "ISO_19005_4", Clause: "6.1.6.1", TestNumber: 2},
	} {
		if !matchStreamFileKeys(compliance.Violation{Rule: rule}) {
			t.Errorf("must claim %s/%s t%d", rule.Specification, rule.Clause, rule.TestNumber)
		}
	}

	for _, rule := range []compliance.Rule{
		{Specification: "ISO_19005_4", Clause: "6.1.7", TestNumber: 1},   // CosUnicodeName, NOT this rule
		{Specification: "ISO_19005_1", Clause: "6.1.7", TestNumber: 1},   // /Length mismatch
		{Specification: "ISO_19005_1", Clause: "6.1.7", TestNumber: 2},   // stream-keyword EOL
		{Specification: "ISO_19005_2", Clause: "6.1.7.1", TestNumber: 1}, // /Length mismatch
		{Specification: "ISO_19005_4", Clause: "6.1.6.1", TestNumber: 1}, // /Length mismatch
		{Specification: "ISO_19005_4", Clause: "6.1.6.1", TestNumber: 3}, // 3D stream /Subtype
		{Specification: "ISO_19005_2", Clause: "6.1.8", TestNumber: 1},
	} {
		if matchStreamFileKeys(compliance.Violation{Rule: rule}) {
			t.Errorf("must not claim %s/%s t%d", rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}

func TestParseStreamKeysContext(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in     string
		wantN  int
		wantK  []string
		wantOK bool
	}{
		{"object 13: stream dict contains [F]", 13, []string{"F"}, true},
		{"object 13: stream dict contains [F FFilter]", 13, []string{"F", "FFilter"}, true},
		{"object 7: stream dict contains [F FFilter FDecodeParms]", 7, []string{"F", "FFilter", "FDecodeParms"}, true},
		{"object 0: stream dict contains [F]", 0, nil, false},
		{"object 13: stream dict contains [Length]", 0, nil, false},
		{"object 13: stream dict contains []", 0, nil, false},
		{"object 13: declared /Length=8 does not match the stream data extent", 0, nil, false},
		{"", 0, nil, false},
	} {
		gotN, gotK, ok := parseStreamKeysContext(tc.in)
		if ok != tc.wantOK || gotN != tc.wantN || len(gotK) != len(tc.wantK) {
			t.Errorf("parseStreamKeysContext(%q) = (%d, %v, %v), want (%d, %v, %v)",
				tc.in, gotN, gotK, ok, tc.wantN, tc.wantK, tc.wantOK)
			continue
		}
		for i, k := range gotK {
			if string(k) != tc.wantK[i] {
				t.Errorf("parseStreamKeysContext(%q) key %d = %q, want %q", tc.in, i, k, tc.wantK[i])
			}
		}
	}
}
