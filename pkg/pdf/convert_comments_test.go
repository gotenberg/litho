package pdf

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// commentsODT is a flat ODT with a comment on a range, its reply and a
// resolved comment on a point.
func commentsODT() string {
	ann := func(name, who, text, extra string) string {
		n := ""
		if name != "" {
			n = ` office:name="` + name + `"`
		}
		return `<office:annotation` + n + extra + `><dc:creator>` + who + `</dc:creator><dc:date>2026-03-01T10:30:00</dc:date><text:p>` + text + `</text:p></office:annotation>`
	}
	return wordODT("", `<text:p>Revenue `+ann("c", "Ada", "Is this right?", "")+ann("r", "Grace", "Yes.", ` loext:parent-name="c"`)+
		`grew<office:annotation-end office:name="c"/> twelve`+ann("", "Alan", "Done.", ` loext:resolved="true"`)+` percent.</text:p>`)
}

// TestConvert_Comments checks that WithComments writes the comments of an
// ODT source as Text annotations, with the reply and the resolved state
// of their threads, and that without it, the default, the PDF holds none.
func TestConvert_Comments(t *testing.T) {
	t.Parallel()
	fonts := WithConvertFontDirs(convertFontDir(t))
	src := strings.Replace(commentsODT(), `office:version="1.3"`, `xmlns:loext="urn:org:documentfoundation:names:experimental:office:xmlns:loext:1.0" office:version="1.3"`, 1)
	for _, tc := range []struct {
		name string
		c    *Conversion
		want []string
	}{
		{"annotations", FromODT(strings.NewReader(src), fonts, WithComments(models.CommentsAnnotations)), []string{"Text Is this right?", "Text Yes.", "Text Done.", "Text Completed"}},
		{"hidden", FromODT(strings.NewReader(src), fonts, WithComments(models.CommentsHidden)), nil},
		{"default", FromODT(strings.NewReader(src), fonts), nil},
	} {
		ctx := context.Background()
		var out bytes.Buffer
		if _, err := tc.c.WriteTo(ctx, &out); err != nil {
			t.Fatalf("%s: WriteTo: %v", tc.name, err)
		}
		r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
		if err != nil {
			t.Fatalf("%s: Open: %v", tc.name, err)
		}
		annots, err := r.ExtractAnnotations(ctx, 1)
		r.Close()
		if err != nil {
			t.Fatalf("%s: ExtractAnnotations: %v", tc.name, err)
		}
		var got []string
		for _, a := range annots {
			got = append(got, a.Subtype+" "+a.Contents)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: annotations %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestConvert_CommentsRemediate checks that a conversion with comments
// remediates to PDF/A-2b and PDF/UA-1 with nothing left.
func TestConvert_CommentsRemediate(t *testing.T) {
	t.Parallel()
	src := strings.Replace(commentsODT(), `office:version="1.3"`, `xmlns:loext="urn:org:documentfoundation:names:experimental:office:xmlns:loext:1.0" office:version="1.3"`, 1)
	c := FromODT(strings.NewReader(src), WithConvertFontDirs(convertFontDir(t)), WithComments(models.CommentsAnnotations), WithDefaultLanguage("en"))
	out, report, err := convertAndRemediate(context.Background(), c, "1.7", models.PDFA2b, models.PDFUA1, "en", "Comments")
	if err != nil {
		t.Fatalf("remediating: %v", err)
	}
	for _, v := range report.ResidualViolations() {
		t.Errorf("residual: %s %s: %s", v.Specification, v.Clause, v.Description)
	}
	r, err := Open(context.Background(), bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if annots, err := r.ExtractAnnotations(context.Background(), 1); err != nil || len(annots) != 4 {
		t.Errorf("remediated output: %d annotations, %v", len(annots), err)
	}
}
