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
