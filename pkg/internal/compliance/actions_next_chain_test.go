package compliance

import (
	"context"
	"strings"
	"testing"
)

// TestActionNextChainJudged pins that every action a /Next tree reaches is
// judged, at every action site, and that each action object is judged once per
// run however many parents reach it. ISO 32000-1, 12.6.2, Table 193 lets /Next
// hold one action dictionary or an array of them, and ISO 19005-1 6.6.1 and
// ISO 19005-2 6.5.1 restrict each action the document performs. veraPDF links
// every /Next dictionary as a PDAction and visits each indirect object once.
//
// # Corpus evidence
//
//   - pdfjs issue20914.pdf: 503 outline GoTo actions whose /Next is a
//     JavaScript action, which veraPDF reports and litho did not.
//   - pdfjs issue18030.pdf: four link annotations share one GoToE action,
//     which veraPDF reports once at PDF/A-1b and litho reported four times.
func TestActionNextChainJudged(t *testing.T) {
	t.Parallel()
	const (
		js     = "<< /S /JavaScript /JS (1) >>"
		launch = "<< /S /Launch /F (x) >>"
		link   = "<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A 6 0 R >>"
	)

	tests := []struct {
		name    string
		profile string
		part    int
		annots  string // page /Annots entries
		a       string // the outline item's /A
		o6, o7  string
		o8, o9  string
		rule    Rule
		want    int
		ctx     string // a context one finding must carry, when set
	}{
		{
			name: "outline GoTo with a JavaScript /Next", profile: "PDF/A-2b", part: 2,
			a:  "<< /S /GoTo /D [3 0 R /Fit] /Next " + js + " >>",
			o6: "<< >>", o7: "<< >>", o8: "<< >>", o9: "<< >>",
			rule: Rule_19005_2_6_5_1_t01, want: 1, ctx: "JavaScript at outline item /Next",
		},
		{
			name: "outline GoTo with a JavaScript /Next, PDF/A-1", profile: "PDF/A-1b", part: 1,
			a:  "<< /S /GoTo /D [3 0 R /Fit] /Next " + js + " >>",
			o6: "<< >>", o7: "<< >>", o8: "<< >>", o9: "<< >>",
			rule: Rule_19005_1_6_6_1_t01, want: 1,
		},
		{
			name: "outline GoTo with a JavaScript /Next, PDF/A-4 permits it", profile: "PDF/A-4", part: 4,
			a:  "<< /S /GoTo /D [3 0 R /Fit] /Next " + js + " >>",
			o6: "<< >>", o7: "<< >>", o8: "<< >>", o9: "<< >>",
			rule: Rule_19005_4_6_6_1_t01, want: 0,
		},
		{
			name: "a /Next array", profile: "PDF/A-2b", part: 2,
			a:  "<< /S /GoTo /D [3 0 R /Fit] /Next [" + js + " 6 0 R] >>",
			o6: launch, o7: "<< >>", o8: "<< >>", o9: "<< >>",
			rule: Rule_19005_2_6_5_1_t01, want: 2, ctx: "Launch at outline item /Next[1]",
		},
		{
			name: "a named action in /Next", profile: "PDF/A-2b", part: 2,
			a:  "<< /S /GoTo /D [3 0 R /Fit] /Next << /S /Named /N /Print >> >>",
			o6: "<< >>", o7: "<< >>", o8: "<< >>", o9: "<< >>",
			rule: Rule_19005_2_6_5_1_t02, want: 1,
		},
		{
			name: "an action an annotation /A and an outline /Next share", profile: "PDF/A-2b", part: 2,
			annots: "8 0 R",
			a:      "<< /S /GoTo /D [3 0 R /Fit] /Next 6 0 R >>",
			o6:     js, o7: "<< >>", o8: link, o9: "<< >>",
			rule: Rule_19005_2_6_5_1_t01, want: 1,
		},
		{
			name: "an action two annotations share", profile: "PDF/A-1b", part: 1,
			annots: "8 0 R 9 0 R",
			a:      "<< /S /GoTo /D [3 0 R /Fit] >>",
			o6:     "<< /S /GoToE /T << /R /C /N (a) >> >>", o7: "<< >>", o8: link, o9: link,
			rule: Rule_19005_1_6_6_1_t01, want: 1,
		},
		{
			name: "a /Next cycle", profile: "PDF/A-2b", part: 2,
			a:  "6 0 R",
			o6: "<< /S /JavaScript /JS (1) /Next 7 0 R >>", o7: "<< /S /Launch /F (x) /Next [6 0 R 7 0 R] >>",
			o8: "<< >>", o9: "<< >>",
			rule: Rule_19005_2_6_5_1_t01, want: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, cat := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /Outlines 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots ["+tt.annots+"] >>",
				"<< /Type /Outlines /First 5 0 R /Last 5 0 R /Count 1 >>",
				"<< /Title (a) /Parent 4 0 R /A "+tt.a+" >>",
				tt.o6, tt.o7, tt.o8, tt.o9,
			)
			got := checkActionGroup(ctx, &validationCtx{doc: doc, catalog: cat, profile: tt.profile, part: tt.part})
			n := 0
			found := tt.ctx == ""
			for _, v := range got {
				if v.Rule != tt.rule {
					continue
				}
				n++
				if strings.Contains(v.Context, tt.ctx) {
					found = true
				}
			}
			if n != tt.want {
				t.Errorf("%s t%d fired %d times, want %d (%v)", tt.rule.Clause, tt.rule.TestNumber, n, tt.want, got)
			}
			if !found {
				t.Errorf("no finding carries context %q (%v)", tt.ctx, got)
			}
		})
	}
}
