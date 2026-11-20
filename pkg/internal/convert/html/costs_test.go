package html

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/models"
)

// liveHeap returns the bytes the live objects hold, after a collection.
func liveHeap() int64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return int64(m.HeapAlloc)
}

// TestTreeCostsCoverTheTree holds [elementCost], [textNodeCost] and
// [attrCost] to what a parsed and prepared document holds on the heap,
// its text and attribute values aside: the tree of each document of many
// small elements weighs no more than [treeCost] charges.
func TestTreeCostsCoverTheTree(t *testing.T) {
	fonts := testFonts(t)
	const n = 20_000
	for name, unit := range map[string]string{
		"empty elements":         "<div></div>",
		"one attribute":          `<div class="a"></div>`,
		"three attributes":       `<div class="a" id="x" title="t"></div>`,
		"five attributes":        `<div a="1" b="2" c="3" d="4" e="5"></div>`,
		"paragraphs of a word":   "<p>hello</p>",
		"inline and text":        "<p><span>hello</span> world</p>",
		"comments":               "<!-- c --><div></div>",
		"long text and a value":  `<p title="` + strings.Repeat("v", 100) + `">` + strings.Repeat("word ", 20) + "</p>",
		"list items and anchors": `<ul><li><a href="#x">a</a></li><li>b</li></ul>`,
		"headings":               "<h1>a</h1>",
		"tables":                 "<table><tr><td>a</td></tr></table>",
	} {
		src := "<!doctype html><html><body>" + strings.Repeat(unit, n) + "</body></html>"
		before := liveHeap()
		doc, err := parse([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		o, err := Options{}.withDefaults()
		if err != nil {
			t.Fatal(err)
		}
		p, err := prepareDoc(context.Background(), doc, o, fonts, newPrintSetup(o))
		if err != nil {
			t.Fatal(err)
		}
		held := liveHeap() - before
		cost, text := treeCost(doc)
		if held-text > cost {
			t.Errorf("%s: the tree holds %d bytes besides its %d of text, past the %d charged", name, held-text, text, cost)
		}
		runtime.KeepAlive(src)
		runtime.KeepAlive(p)
	}
}

func TestChargeTree_ChargesWhatDecodingGrows(t *testing.T) {
	doc, err := parse([]byte("<p>\xe9\xe9\xe9\xe9</p><meta charset=windows-1252>"))
	if err != nil {
		t.Fatal(err)
	}
	cost, text := treeCost(doc)
	got, err := chargeTree(context.Background(), doc, 4)
	if err != nil {
		t.Fatal(err)
	}
	if text <= 4 || got != cost+text-4 {
		t.Errorf("chargeTree = %d for %d bytes of text from 4 source bytes, want %d", got, text, cost+text-4)
	}
}

// TestConvert_ChargesThePagesItReturns checks that the pages Convert
// returns stay charged once it returns: the caller writes them all
// afterwards.
func TestConvert_ChargesThePagesItReturns(t *testing.T) {
	ctx, mem, done := limits.WithBudget(context.Background(), models.Limits{})
	defer done()
	src := strings.Repeat(`<p style="border:1px solid">a word</p>`, 200)
	doc, err := Convert(ctx, strings.NewReader(src), testFonts(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var pages int64
	for _, pg := range doc.Pages {
		pages += typeset.ContentCost(pg.Content)
	}
	if held := mem.Held(); held < pages {
		t.Errorf("the budget holds %d bytes once Convert returns, under the %d its pages hold", held, pages)
	}
}
