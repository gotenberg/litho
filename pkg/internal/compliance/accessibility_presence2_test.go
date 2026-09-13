package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestFileAndMediaPresence pins three presence tests as veraPDF reads them: a
// file attachment's /AFRelationship counts only as a name
// (getNameKeyStringValue, ISO 14289-2, 8.9.2.4.10 t1), an embedded file
// specification's /Desc counts with any non-null value (knownKey, ISO 14289-2,
// 8.14.1 t1), and a media clip's /CT counts only as a string (getContentType,
// ISO 14289-1, 7.18.6.2 t1). A null value is absent (ISO 32000-1, 7.3.7).
// Every case was measured against veraPDF.
func TestFileAndMediaPresence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	fileAttachment := func(afrel string) []string {
		return []string{
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [7 0 R] >>",
			"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 6 0 R] >> /Namespaces [9 0 R] >>",
			"<< /S /Document /NS 9 0 R /P 4 0 R /K [6 0 R] >>",
			"<< /S /Annot /NS 9 0 R /P 5 0 R /Pg 3 0 R /K [<< /Type /OBJR /Obj 7 0 R /Pg 3 0 R >>] >>",
			"<< /Type /Annot /Subtype /FileAttachment /Rect [10 10 60 60] /F 4 /StructParent 0 /Contents (file) /FS 8 0 R >>",
			"<< /Type /Filespec /F (a.txt) /UF (a.txt) /Desc (d)" + afrel + " >>",
			"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
		}
	}
	embedded := func(desc string) []string {
		return []string{
			"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(a.txt) 4 0 R] >> >>" +
				" /MarkInfo << /Marked true >> /Lang (en) >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
			"<< /Type /Filespec /F (a.txt) /UF (a.txt) /AFRelationship /Data /EF << /F 5 0 R >>" + desc + " >>",
			streamObj("hello"),
		}
	}
	mediaClip := func(ct string) []string {
		return []string{
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [7 0 R] >>",
			"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 6 0 R] >> >>",
			"<< /S /Document /P 4 0 R /K [6 0 R] >>",
			"<< /S /Annot /P 5 0 R /Pg 3 0 R /K [<< /Type /OBJR /Obj 7 0 R /Pg 3 0 R >>] >>",
			"<< /Type /Annot /Subtype /Screen /Rect [10 10 100 100] /F 4 /Contents (a movie) /StructParent 0" +
				" /A << /S /Rendition /OP 0 /R << /Type /Rendition /S /MR /C << /Type /MediaClip /S /MCD" +
				" /D (movie.mp4) /Alt [(en) (a movie)]" + ct + " >> >> >> >>",
		}
	}

	tests := []struct {
		name string
		ua2  bool
		objs []string
		rule Rule
		want int
	}{
		{"an /AFRelationship written as null", true, fileAttachment(" /AFRelationship null"), Rule_14289_2_8_9_2_4_10_t01, 1},
		{"an /AFRelationship that is a string", true, fileAttachment(" /AFRelationship (Data)"), Rule_14289_2_8_9_2_4_10_t01, 1},
		{"an /AFRelationship that is a name", true, fileAttachment(" /AFRelationship /Data"), Rule_14289_2_8_9_2_4_10_t01, 0},
		{"a /Desc written as null", true, embedded(" /Desc null"), Rule_14289_2_8_14_1_t01, 1},
		{"a /Desc of text", true, embedded(" /Desc (d)"), Rule_14289_2_8_14_1_t01, 0},
		{"no /Desc", true, embedded(""), Rule_14289_2_8_14_1_t01, 1},
		{"a /CT written as null", false, mediaClip(" /CT null"), Rule_14289_1_7_18_6_2_t01, 1},
		{"a /CT that is a name", false, mediaClip(" /CT /video"), Rule_14289_1_7_18_6_2_t01, 1},
		{"a /CT of text", false, mediaClip(" /CT (video/mp4)"), Rule_14289_1_7_18_6_2_t01, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, tt.objs...)
			profile := models.PDFUA1
			if tt.ua2 {
				profile = models.PDFUA2
			}
			viols, err := ValidatePDFUA(ctx, doc, profile)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, tt.rule); got != tt.want {
				t.Errorf("%s t%d fired %d times, want %d", tt.rule.Clause, tt.rule.TestNumber, got, tt.want)
			}
		})
	}
}
