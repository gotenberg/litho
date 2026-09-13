package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestNoteIDDecoded pins ISO 14289-1, 7.9 t1 and t2 on the decoded /ID, as
// veraPDF reads it (GFSENote, getStringKey): an /ID holding only a byte order
// mark is empty, and two /ID strings that decode to the same text are the same
// ID whatever their encoding. Every case was measured against veraPDF.
func TestNoteIDDecoded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name   string
		ids    []string // PDF string tokens for the /ID of each Note, in order
		t1, t2 int
	}{
		{"an /ID holding only a byte order mark", []string{"<FEFF>"}, 1, 0},
		{"an /ID of text", []string{"(n1)"}, 0, 0},
		{"one text in two encodings", []string{"<FEFF0041>", "(A)"}, 0, 1},
		{"two distinct IDs", []string{"(A)", "(B)"}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := capNode{"Sect", nil}
			for _, id := range tt.ids {
				root.kids = append(root.kids, capNode{"Note /ID " + id, nil})
			}
			viols, err := ValidatePDFUA(ctx, buildCapDoc(t, root, "<< >>"), models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_1_7_9_t01); got != tt.t1 {
				t.Errorf("7.9 t1 fired %d times, want %d", got, tt.t1)
			}
			if got := countRuleErrs(t, viols, Rule_14289_1_7_9_t02); got != tt.t2 {
				t.Errorf("7.9 t2 fired %d times, want %d", got, tt.t2)
			}
		})
	}
}

// TestEmbeddedFileSpecNamesDecoded pins ISO 14289-1, 7.11 t1 on the decoded /F
// and /UF, as veraPDF reads them (GFCosFileSpecification, getStringKey): a name
// holding only a byte order mark is empty. veraPDF reaches the one file
// specification twice, through the EmbeddedFiles tree and as an indirect
// object, and so reports it twice; litho reports each file specification once.
// The verdicts were measured against veraPDF.
func TestEmbeddedFileSpecNamesDecoded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name  string
		f, uf string
		want  int
	}{
		{"an /F holding only a byte order mark", "<FEFF>", "(a.txt)", 1},
		{"a /UF holding only a byte order mark", "(a.txt)", "<FEFF>", 1},
		{"both names of text", "(a.txt)", "(a.txt)", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(a.txt) 4 0 R] >> >>"+
					" /MarkInfo << /Marked true >> /Lang (en) >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
				"<< /Type /Filespec /F "+tt.f+" /UF "+tt.uf+" /EF << /F 5 0 R >> /AFRelationship /Unspecified /Desc (d) >>",
				streamObj("hello"),
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_1_7_11_t01); got != tt.want {
				t.Errorf("7.11 t1 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
