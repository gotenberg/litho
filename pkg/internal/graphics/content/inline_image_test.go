package content

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// TestInlineImagesReadAsVeraPDF pins how a parse without an option reads the
// data of inline images, as veraPDF does (see [inlineImages]). Every stream
// was measured with veraPDF 1.30 as the content of a one-page file at PDF/A-1b
// and PDF/A-2b: the undefined operators it reports are the operations below
// outside Annex A, Table A.1, EI and ID included ([IsUndefined]), and it
// judges the colour space, filter and interpolation of the first inline image
// alone.
func TestInlineImagesReadAsVeraPDF(t *testing.T) {
	t.Parallel()

	const g = "BI /W 1 /H 1 /BPC 8 /CS /G "
	for _, tc := range []struct {
		name, data string
		want       []string
	}{
		{"two in a row", "BI /F /LZW ID xx EI\nBI /F /LZW ID yy EI\n", []string{"BI"}},
		{"a space between", "BI /F /LZW ID xx EI BI /F /LZW ID yy EI\n", []string{"BI"}},
		{"comments between", "BI /F /LZW ID xx EI\n%c\n%d\nBI /F /LZW ID yy EI\n", []string{"BI"}},
		{"an operator between", g + "ID xyz\nEI\nq Q\n" + g + "ID xyz\nEI\n", []string{"BI", "q", "Q", "BI"}},
		{"three in a row", "BI /F /LZW ID xx EI\nBI /F /LZW ID yy EI\nBI /F /LZW ID zz EI\n", []string{"BI"}},
		{"the first shorter", "BI /F /LZW ID x EI\nBI /F /LZW ID yyyy EI\n", []string{"BI", "EI"}},
		{"the first longer", "BI /F /LZW ID xxxxxxxxxx EI\nBI /F /LZW ID y EI\nzz zz zz ww\n", []string{"BI", "ww"}},
		{"three of varied lengths", "BI /F /LZW ID xx EI\nBI /F /LZW ID yyyyyy EI\nBI /F /LZW ID z EI\nzz ww\n", []string{"BI", "zz", "ww"}},
		{"the last with no end", "BI /F /LZW ID x EI\nBI /F /LZW ID yyyyyyy EI\nzz\n", []string{"BI", "yyy", "EI", "zz"}},
		{"a comment then no operator", g + "ID xEI %c\nzz EI\n", []string{"BI"}},
		{"no operator after an EI", g + "ID x EI zz EI\n", []string{"BI"}},
		{"an EI after data", g + "ID xxEI Q zz\n", []string{"BI", "Q", "zz"}},
		{"an EI before the length", "BI /W 1 /H 1 /BPC 8 /CS /G /L 8 ID xx EI q EI\nQ zz\n", []string{"BI", "Q", "zz"}},
		{"no length", g + "ID xx EI q EI\nQ zz\n", []string{"BI", "q", "EI", "Q", "zz"}},
		{"no EI", g + "ID xx\nzz\n", []string{"BI"}},
		{"an EI at the end", g + "ID xx EI", []string{"BI"}},
		{"an EI after an EI", g + "ID xx EI EI\nzz\n", []string{"BI", "EI", "zz"}},
		{"a BI with no ID after an EI", g + "ID xx EI BI /W 1 zz EI\nQ\n", []string{"BI", "Q"}},
		{"n0 after an EI", g + "ID xx EI n0 EI\nQ\n", []string{"BI", "Q"}},
		{"an operator run through a comment", g + "ID xx EI Q%c\nEI\nQ\n", []string{"BI", "Q"}},
		{"B0 after an EI", g + "ID xx EI B0 EI\nQ\n", []string{"BI", "Q"}},
		{"a stray delimiter after an EI", g + "ID xx EI )x EI\nQ\n", []string{"BI", "Q"}},
		{"a stray EI", "q EI Q\n", []string{"q", "EI", "Q"}},
		{"an ID no BI begins", "q ID xx EI\nQ zz\n", []string{"q", "ID", "Q", "zz"}},
		{"an image after an ID", "q ID xx EI\nBI /W 1 /H 1 /BPC 8 /CS /RGB ID xy EI\nQ\n", []string{"q", "ID", "Q"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := operatorsOf(t, tc.data); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("operators = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestInlineImagesReadWholeByOtherReadings pins that the readings that follow
// what a viewer draws, or what a repair writes back, read every inline image
// of a run, as they read past the other points where veraPDF stops.
func TestInlineImagesReadWholeByOtherReadings(t *testing.T) {
	t.Parallel()

	data := "BI /F /LZW ID x EI\nBI /F /LZW ID yyyy EI\n"
	want := []string{"BI", "BI"}
	for name, opt := range map[string]Option{
		"WholeStream":         WholeStream(),
		"ForRepair":           ForRepair(),
		"SplitGluedOperators": SplitGluedOperators(),
	} {
		if got := operatorsOf(t, data, opt); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: operators = %q, want %q", name, got, want)
		}
	}
	ops, err := ParseStreamData(WithWholeStreams(context.Background()), []byte(data))
	if err != nil || len(ops) != len(want) {
		t.Errorf("WithWholeStreams: %d operations, %v, want %d", len(ops), err, len(want))
	}
}

// TestInlineImagesReadByRepairs pins that the readings of a repair end the
// data of an inline image at the later of the ends veraPDF and PDF.js give it
// ([inlineImages.repairEnd]), so no byte either reads as data reads as an
// operator.
func TestInlineImagesReadByRepairs(t *testing.T) {
	t.Parallel()

	const g = "BI /W 1 /H 1 /BPC 8 /CS /G "
	for _, tc := range []struct {
		name, data          string
		vera, pdfjs, repair []string
	}{
		// veraPDF reads an EI before the /L bytes as data.
		{"veraPDF ends later", g + "/L 9 ID x EI Q zz EI Q\n", []string{"BI", "Q"}, []string{"BI", "Q", "zz", "EI", "Q"}, []string{"BI", "Q"}},
		// PDF.js takes no EI that a tab follows.
		{"PDF.js ends later", g + "ID x EI\tQ zz EI Q\n", []string{"BI", "Q", "zz", "EI", "Q"}, []string{"BI", "Q"}, []string{"BI", "Q"}},
		{"both end at an EI against the data", g + "ID xEI Q\n", []string{"BI", "Q"}, []string{"BI", "Q"}, []string{"BI", "Q"}},
		{"no EI", g + "ID x\nQ\n", []string{"BI"}, []string{"BI"}, []string{"BI"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := operatorsOf(t, tc.data); !reflect.DeepEqual(got, tc.vera) {
				t.Errorf("read as veraPDF reads it: %q, want %q", got, tc.vera)
			}
			if got := operatorsOf(t, tc.data, SplitGluedOperators()); !reflect.DeepEqual(got, tc.pdfjs) {
				t.Errorf("read as PDF.js reads it: %q, want %q", got, tc.pdfjs)
			}
			for name, opt := range map[string]Option{"ForRepair": ForRepair(), "WholeStream": WholeStream()} {
				if got := operatorsOf(t, tc.data, opt); !reflect.DeepEqual(got, tc.repair) {
					t.Errorf("%s: %q, want %q", name, got, tc.repair)
				}
			}
			ends := NewInlineImageEnds([]byte(tc.data))
			idEnd := strings.Index(tc.data, "ID") + len("ID")
			if got, want := ends.End(context.Background(), idEnd, nil), newInlineImages([]byte(tc.data)).repairEnd(context.Background(), idEnd, nil); got != want {
				t.Errorf("InlineImageEnds.End = %d, want %d", got, want)
			}
		})
	}
}

// TestInlineImagesResumeOffsets pins where the reading resumes after an image
// whose EI a BI follows: at the data of the last image read with it, offset
// by the length of the data of the first, "EI" included.
func TestInlineImagesResumeOffsets(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		data string
		want int
	}{
		// The data "x EI" runs from 3 to 7.
		{"ID x EI\nQ", 7},
		{"ID x EI\nBI ID yy EI\nQ", len("ID x EI\nBI ID ") + len("x EI")},
		{"ID xx EI\nBI /W 1 ID yy EI\nQ", len("ID xx EI\nBI /W 1 ID yy EI")},
		// An ID after an EI reads the data after it the same way.
		{"ID x EI ID yyy EI Q", len("ID x EI ID ") + len("x EI")},
		{"ID xx", len("ID xx")},
		{"ID", len("ID")},
	} {
		r := newInlineImages([]byte(tc.data))
		if got := r.resume(context.Background(), len("ID"), nil); got != tc.want {
			t.Errorf("resume(%q) = %d, want %d", tc.data, got, tc.want)
		}
	}
}

// TestInlineImagesCheckRemembersASkippedComment pins the first of the two
// guards [inlineImages] describes: an EI inside a comment another check has
// skipped meets the token past it without skipping the comment again, and
// reads the same answer.
func TestInlineImagesCheckRemembersASkippedComment(t *testing.T) {
	t.Parallel()

	data := "ID x EI %" + strings.Repeat("EI %", 1000) + "\nzz EI\nQ\n"
	r := newInlineImages([]byte(data))
	first := strings.Index(data, "EI %") + 2
	if c := r.check(context.Background(), first); c.ends {
		t.Fatal("an EI followed by a comment and zz ends the data")
	}
	skipTo := r.skipTo
	if want := strings.Index(data, "zz"); skipTo != want {
		t.Fatalf("the skipped run ends at %d, want %d", skipTo, want)
	}
	// Break the remembered token: a check that read past the comment again
	// would now see an operator.
	r.data = []byte(strings.Replace(data, "zz", "q ", 1))
	if c := r.check(context.Background(), first+4); c.ends {
		t.Error("an EI inside the skipped comment read past it again")
	}
}

// TestInlineImagesHeaderBudget pins the second guard [inlineImages]
// describes: the dictionaries the checks read take at most the length of the
// stream in all, past which a BI after an EI reads as no image.
func TestInlineImagesHeaderBudget(t *testing.T) {
	t.Parallel()

	data := []byte("BI /K [ " + strings.Repeat("EI BI /K [ ", 100))
	r := newInlineImages(data)
	for q := 0; r.budget > 0; q++ {
		if q > len(data) {
			t.Fatal("the budget never ran out")
		}
		if next := r.header(context.Background(), len("BI")); next != nil {
			t.Fatal("a dictionary with no ID read as an image")
		}
	}
	good := newInlineImages([]byte("BI /W 1 ID x EI"))
	if good.header(context.Background(), len("BI")) == nil {
		t.Fatal("a dictionary that ends in ID read as no image")
	}
	good.budget = 0
	if good.header(context.Background(), len("BI")) != nil {
		t.Error("a dictionary read past the budget")
	}
}

// TestIsUndefined pins the operators ISO 19005 forbids in a content stream
// as a parse returns them: those outside Annex A, Table A.1, and EI and ID,
// which stand for themselves only outside an inline image.
func TestIsUndefined(t *testing.T) {
	t.Parallel()

	for _, op := range []string{"EI", "ID", "zz", "ETBT", ""} {
		if !IsUndefined(op) {
			t.Errorf("IsUndefined(%q) = false", op)
		}
	}
	for _, op := range []string{"BI", "q", "Tj", "d0", "'"} {
		if IsUndefined(op) {
			t.Errorf("IsUndefined(%q) = true", op)
		}
	}
}
