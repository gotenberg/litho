package compliance

import (
	"context"
	"testing"
	"time"
)

// TestChainedGoToStructureDestinations pins ISO 14289-2, 8.8 t2 on the GoTo
// actions an action runs after it through /Next (ISO 32000-1, 12.6.2, Table
// 193): one action or an array of them, at every site that holds an action.
// Each action object is judged once, as veraPDF judges it: an action two sites
// share, or a /Next that loops back, adds no finding. Every case was measured
// against veraPDF.
func TestChainedGoToStructureDestinations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const (
		goTo   = "<< /S /GoTo /D [3 0 R /Fit] >>"
		sdGoTo = "<< /S /GoTo /D [3 0 R /Fit] /SD [5 0 R /Fit] >>"
		link   = "<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] "
	)

	tests := []struct {
		name  string
		cat   string   // entries on the catalog
		page  string   // entries on the page
		item  string   // entries on the outline item, object 8
		extra []string // objects 9 onwards
		t2    int
	}{
		{
			name:  "a GoTo after a Named action under a page's /AA",
			page:  "/AA << /O 9 0 R >>",
			extra: []string{"<< /S /Named /N /FirstPage /Next " + goTo + " >>"},
			t2:    1,
		},
		{
			name:  "a /Next array after an outline item's GoTo",
			cat:   "/Outlines 7 0 R",
			item:  "/A << /S /GoTo /D [3 0 R /Fit] /Next [<< /S /Named /N /NextPage >> 9 0 R] >>",
			extra: []string{goTo},
			t2:    2,
		},
		{
			name: "a /Next cycle through a URI action",
			page: "/Annots [9 0 R]",
			extra: []string{
				link + "/A 10 0 R >>",
				"<< /S /GoTo /D [3 0 R /Fit] /Next 11 0 R >>",
				"<< /S /URI /URI (https://example.com/) /Next 12 0 R >>",
				"<< /S /GoTo /D [3 0 R /Fit] /Next 10 0 R >>",
			},
			t2: 2,
		},
		{
			name:  "a GoTo the OpenAction and an outline item both run",
			cat:   "/Outlines 7 0 R /OpenAction << /S /GoTo /D [3 0 R /Fit] /Next 9 0 R >>",
			item:  "/A << /S /GoTo /D [3 0 R /Fit] /Next 9 0 R >>",
			extra: []string{goTo},
			t2:    3,
		},
		{
			name: "a /Next array of its own after a link's Named action",
			page: "/Annots [9 0 R]",
			extra: []string{
				link + "/A << /S /Named /N /NextPage /Next 10 0 R >> >>",
				"[" + goTo + " " + sdGoTo + "]",
			},
			t2: 1,
		},
		{
			name:  "a GoTo after a Named action under a link's /AA",
			page:  "/Annots [9 0 R]",
			extra: []string{link + "/AA << /E << /S /Named /N /NextPage /Next " + goTo + " >> >> >>"},
			t2:    1,
		},
		{
			name: "a GoTo two links run and a third runs after its own",
			page: "/Annots [9 0 R 10 0 R 11 0 R]",
			extra: []string{
				link + "/A 12 0 R >>",
				link + "/A 12 0 R >>",
				link + "/A << /S /GoTo /D [3 0 R /Fit] /SD [5 0 R /Fit] /Next 12 0 R >> >>",
				goTo,
			},
			t2: 1,
		},
		{
			name: "a GoTo with /SD after the OpenAction's",
			cat:  "/OpenAction << /S /GoTo /D [3 0 R /Fit] /SD [5 0 R /Fit] /Next " + sdGoTo + " >>",
			t2:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			objs := append([]string{
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en) " + tt.cat + " >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Tabs /S " + tt.page + " >>",
				"<< /Type /StructTreeRoot /K 5 0 R /Namespaces [6 0 R] >>",
				"<< /Type /StructElem /S /Document /NS 6 0 R /P 4 0 R >>",
				"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
				"<< /Type /Outlines /First 8 0 R /Last 8 0 R /Count 1 >>",
				"<< /Title (x) /Parent 7 0 R " + tt.item + " >>",
			}, tt.extra...)
			doc, cat := objsDocPDF(t, objs...)
			got := checkStructureDestinations(ctx, doc, cat, "PDF/UA-2")
			if n := countRule(got, Rule_14289_2_8_8_t02); n != tt.t2 {
				t.Errorf("8.8 t2 fired %d times, want %d: %v", n, tt.t2, got)
			}
			if n := countRule(got, Rule_14289_2_8_8_t01); n != 0 {
				t.Errorf("8.8 t1 fired %d times, want 0: %v", n, got)
			}
		})
	}
}

// TestChainedGoToStructureDestinationsFanOut pins that a /Next array written
// as an object of its own, whose direct actions each run it again, is walked
// once: walked anew wherever it is reached, it would fan out 2^depth.
func TestChainedGoToStructureDestinationsFanOut(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /OpenAction << /S /Named /N /NextPage /Next 4 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"[<< /S /GoTo /D [3 0 R /Fit] /Next 4 0 R >> << /S /GoTo /D [3 0 R /Fit] /Next 4 0 R >>]",
	)
	got := checkStructureDestinations(ctx, doc, cat, "PDF/UA-2")
	if ctx.Err() != nil {
		t.Fatal("the walk did not end before the deadline")
	}
	if n := countRule(got, Rule_14289_2_8_8_t02); n != 2 {
		t.Errorf("8.8 t2 fired %d times, want 2, one per direct GoTo action: %v", n, got)
	}
}
