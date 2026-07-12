package compliance

import (
	"context"
	"testing"
)

func TestMimeTypePattern(t *testing.T) {
	valid := []string{"application/pdf", "image/png", "text/plain", "application/vnd.ms-excel", "application/x-foo+bar"}
	invalid := []string{"", "pdf", "application/", "/pdf", "a b/c", "application/pdf "}
	for _, s := range valid {
		if !mimeTypePattern.MatchString(s) {
			t.Errorf("mimeTypePattern should match %q", s)
		}
	}
	for _, s := range invalid {
		if mimeTypePattern.MatchString(s) {
			t.Errorf("mimeTypePattern should not match %q", s)
		}
	}
}

func TestEmbeddedSubtypeMIME(t *testing.T) {
	ctx := context.Background()
	mk := func(subtype string) []Violation {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(f.txt) 4 0 R] >> >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /Filespec /F (f.txt) /UF (f.txt) /EF << /F 5 0 R >> /AFRelationship /Data >>",
			"<< /Type /EmbeddedFile /Subtype /"+subtype+" /Length 3 >>\nstream\nabc\nendstream",
		)
		var out []Violation
		for _, fs := range collectFileSpecs(ctx, doc) {
			out = append(out, checkEmbeddedSubtypeMIME(ctx, doc, fs)...)
		}
		return out
	}

	if got := mk("text#2Fplain"); hasRule(got, Rule_19005_3_6_8_t01) {
		t.Errorf("valid MIME text/plain: got %v, want none", got)
	}
	if got := mk("badmime"); !hasRule(got, Rule_19005_3_6_8_t01) {
		t.Errorf("invalid MIME without slash: got %v, want includes §6.8 t1", got)
	}
}

func TestMetadataTypeSubtype(t *testing.T) {
	ctx := context.Background()
	mk := func(metaObj string) []Violation {
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			metaObj,
		)
		return checkMetadataTypeSubtype(ctx, doc, cat, "PDF/UA-1")
	}

	good := "<< /Type /Metadata /Subtype /XML /Length 3 >>\nstream\nabc\nendstream"
	if got := mk(good); len(got) != 0 {
		t.Errorf("valid /Metadata stream: got %v, want none", got)
	}
	if got := mk("<< /Type /Foo /Subtype /XML /Length 3 >>\nstream\nabc\nendstream"); !hasRule(got, Rule_14289_1_7_1_t08) {
		t.Errorf("wrong /Type: got %v, want includes §7.1 t8", got)
	}
	if got := mk("<< /Type /Metadata /Subtype /RDF /Length 3 >>\nstream\nabc\nendstream"); !hasRule(got, Rule_14289_1_7_1_t08) {
		t.Errorf("wrong /Subtype: got %v, want includes §7.1 t8", got)
	}

	// UA-2 emits its own rule; a non-UA profile emits nothing.
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Foo /Length 3 >>\nstream\nabc\nendstream",
	)
	if got := checkMetadataTypeSubtype(ctx, doc, cat, "PDF/UA-2"); !hasRule(got, Rule_14289_2_8_11_1_t02) {
		t.Errorf("UA-2 wrong /Type: got %v, want includes §8.11.1 t2", got)
	}
	if got := checkMetadataTypeSubtype(ctx, doc, cat, "PDF/A-2b"); len(got) != 0 {
		t.Errorf("non-UA profile: got %v, want none", got)
	}
}

// TestCheckEmbeddedFileMIME_KidsNested pins PDF/A-4 §6.9 t1 for a file
// specification reachable only through an intermediate /Kids node of the
// EmbeddedFiles name tree: the check walks every /Type /Filespec object via
// collectFileSpecs, so nesting cannot hide a stream with a missing or invalid
// /Subtype. A direct /Names read (the previous implementation) missed these.
func TestCheckEmbeddedFileMIME_KidsNested(t *testing.T) {
	ctx := context.Background()
	mk := func(stream string) []Violation {
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Kids [4 0 R] >> >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Names [(f.dat) 5 0 R] >>", // intermediate /Kids node
			"<< /Type /Filespec /F (f.dat) /EF << /F 6 0 R >> /AFRelationship /Data >>",
			stream,
		)
		return checkEmbeddedFileMIME(ctx, doc, cat)
	}
	// Stream with no /Subtype at all: fires §6.9 t1.
	noSub := "<< /Type /EmbeddedFile /Length 3 >>\nstream\nabc\nendstream"
	if got := mk(noSub); !hasRule(got, Rule_19005_4_6_9_t01) {
		t.Errorf("nested filespec with missing /Subtype: want §6.9 t1, got %v", got)
	}
	// Stream with a valid MIME /Subtype: no violation.
	goodSub := "<< /Type /EmbeddedFile /Subtype /text#2Fplain /Length 3 >>\nstream\nabc\nendstream"
	if got := mk(goodSub); hasRule(got, Rule_19005_4_6_9_t01) {
		t.Errorf("nested filespec with valid MIME: want none, got %v", got)
	}
}
