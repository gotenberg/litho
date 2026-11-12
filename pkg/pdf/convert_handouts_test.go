package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// handoutODP is a presentation of seven 28 by 15.75 cm slides named S1 to
// S7, the fourth hidden, the first linking to the sixth, with a handout
// master on an A4 portrait page layout whose footer frame shows the page
// number.
func handoutODP() string {
	var pages strings.Builder
	for i := 1; i <= 7; i++ {
		style := ""
		if i == 4 {
			style = ` draw:style-name="hidden"`
		}
		text := fmt.Sprintf("Slide %d", i)
		if i == 1 {
			text = `<text:a xmlns:xlink="http://www.w3.org/1999/xlink" xlink:href="#S6">Slide 1</text:a>`
		}
		fmt.Fprintf(&pages, `<draw:page draw:name="S%d" draw:master-page-name="Default"%s><draw:frame svg:x="1cm" svg:y="1cm" svg:width="20cm" svg:height="3cm"><draw:text-box><text:p>%s</text:p></draw:text-box></draw:frame></draw:page>`, i, style, text)
	}
	return flatODF("presentation",
		pageLayout("PM1", "28cm", "15.75cm", "")+pageLayout("PM2", "21cm", "29.7cm", "")+
			`<style:style style:name="hidden" style:family="drawing-page"><style:drawing-page-properties presentation:visibility="hidden"/></style:style>`+
			`<style:style style:name="hm" style:family="drawing-page"><style:drawing-page-properties presentation:display-page-number="true"/></style:style>`,
		`<style:handout-master style:page-layout-name="PM2" draw:style-name="hm"><draw:frame presentation:class="page-number" svg:x="12cm" svg:y="28cm" svg:width="9cm" svg:height="1.5cm"><draw:text-box><text:p>Page <text:page-number>1</text:page-number></text:p></draw:text-box></draw:frame></style:handout-master>`+
			`<style:master-page style:name="Default" style:page-layout-name="PM1"/>`,
		`<office:presentation>`+pages.String()+`</office:presentation>`)
}

// TestConvert_Handouts checks every count of slides a handout page and
// both orientations: one handout page for each count of shown slides, the
// last holding the rest, on the handout master's A4 page or turned; the
// bookmarks and the link to a slide leading to the handout page that holds
// it; the handout master's page number counting the handout pages.
func TestConvert_Handouts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fonts := WithConvertFontDirs(convertFontDir(t))
	shown := []string{"S1", "S2", "S3", "S5", "S6", "S7"}
	for _, per := range []int{1, 2, 3, 4, 6, 9} {
		for _, o := range []Orientation{OrientationPortrait, OrientationLandscape} {
			t.Run(fmt.Sprintf("%d/%d", per, o), func(t *testing.T) {
				t.Parallel()
				var out bytes.Buffer
				rep, err := FromODP(strings.NewReader(handoutODP()), fonts, WithHandouts(per), WithOrientation(o)).WriteTo(ctx, &out)
				if err != nil {
					t.Fatal(err)
				}
				pages := (len(shown) + per - 1) / per
				if rep.Pages != pages {
					t.Fatalf("%d pages, want %d", rep.Pages, pages)
				}
				r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				w, h := a4W, a4H
				if o == OrientationLandscape {
					w, h = h, w
				}
				for i := 1; i <= pages; i++ {
					p, err := r.Page(ctx, i)
					if err != nil {
						t.Fatal(err)
					}
					checkSizes(t, fmt.Sprint("handout page ", i), [][2]float64{{p.Width, p.Height}}, 1, w, h)
					// The page number is a pagination artifact.
					text, err := r.ExtractText(ctx, i, WithHiddenText())
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(text, fmt.Sprintf("Page %d", i)) {
						t.Errorf("page %d shows no page number: %q", i, text)
					}
					for j := (i - 1) * per; j < min(i*per, len(shown)); j++ {
						if n := shown[j][1:]; !strings.Contains(text, "Slide "+n) {
							t.Errorf("page %d lacks slide %s: %q", i, n, text)
						}
					}
				}
				marks, err := r.Bookmarks(ctx)
				if err != nil || len(marks) != len(shown) {
					t.Fatalf("bookmarks %+v, %v", marks, err)
				}
				for i, m := range marks {
					if m.Title != shown[i] || m.PageNumber != i/per+1 {
						t.Errorf("bookmark %d: %q to page %d, want %q to %d", i, m.Title, m.PageNumber, shown[i], i/per+1)
					}
				}
				annots, err := r.ExtractAnnotations(ctx, 1)
				if err != nil {
					t.Fatal(err)
				}
				if len(annots) != 1 || annots[0].DestPage != 4/per+1 {
					t.Errorf("link of page 1 %+v, want one to page %d", annots, 4/per+1)
				}
			})
		}
	}
}

// TestConvert_HandoutsSelectAndFit checks that a page range selects
// handout pages, that a paper fits the handout page and that a paper of
// the other orientation turns it.
func TestConvert_HandoutsSelectAndFit(t *testing.T) {
	t.Parallel()
	fonts := WithConvertFontDirs(convertFontDir(t))
	_, sizes := conversionSizes(t, FromODP(strings.NewReader(handoutODP()), fonts, WithHandouts(2), WithPageRanges("2-3")))
	checkSizes(t, "handout pages 2 and 3", sizes, 2, a4W, a4H)
	_, sizes = conversionSizes(t, FromODP(strings.NewReader(handoutODP()), fonts, WithHandouts(4), WithPaper(PaperLetter)))
	checkSizes(t, "handouts on Letter", sizes, 2, letterW, letterH)
	// A paper on its side turns the handout page: six slides on one page
	// that fills the paper's width.
	_, sizes = conversionSizes(t, FromODP(strings.NewReader(handoutODP()), fonts, WithHandouts(6), WithPaper(Paper{Width: letterH, Height: letterW})))
	checkSizes(t, "handouts on Letter on its side", sizes, 1, letterH, letterW)
}

// TestConvert_PPTXHandouts checks that the handout pages of a PPTX source
// take its notes size, 540 by 720 points for the handout-master fixture,
// and that handouts beside notes pages fail.
func TestConvert_PPTXHandouts(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "test", "integration", "testdata", "litho", "pptx", "sources", "handout-master", "handout-master.pptx")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("handout-master.pptx is not built: make corpus-build CORPUS=litho/pptx")
	}
	if err != nil {
		t.Fatal(err)
	}
	fonts := WithConvertFontDirs(convertFontDir(t))
	_, sizes := conversionSizes(t, FromPPTX(bytes.NewReader(data), fonts, WithHandouts(2)))
	checkSizes(t, "handouts", sizes, 2, 540, 720)
	_, err = FromPPTX(bytes.NewReader(data), fonts, WithHandouts(3), WithNotesPages(true)).WriteTo(context.Background(), &bytes.Buffer{})
	if !errors.Is(err, ErrInvalidConvertOptions) {
		t.Errorf("handouts with notes pages: %v, want ErrInvalidConvertOptions", err)
	}
}
