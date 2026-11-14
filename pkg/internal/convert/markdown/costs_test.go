package markdown

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	md "github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// mustRender renders src as Convert does, failing the test on error.
func mustRender(t testing.TB, src []byte) ([]byte, string) {
	t.Helper()
	body, title, err := render(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	return body, title
}

// liveHeap returns the bytes the live objects hold, after a collection.
func liveHeap() int64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return int64(m.HeapAlloc)
}

// TestASTNodeCostCoversANode holds [astNodeCost] to what a node of the
// parsed Markdown tree holds on the heap, for documents of many small
// blocks and spans of each kind.
func TestASTNodeCostCoversANode(t *testing.T) {
	const n = 20_000
	for name, unit := range map[string]string{
		"paragraphs":  "word\n\n",
		"list items":  "- a\n",
		"headings":    "# h\n\n",
		"emphases":    "*a* ",
		"code spans":  "`x` ",
		"links":       "[a](b) ",
		"quotes":      "> q\n\n",
		"table rows":  "| a | b |\n",
		"fenced code": "```\nx\n```\n\n",
	} {
		src := []byte(strings.Repeat(unit, n))
		if name == "table rows" {
			src = []byte("| h | k |\n|---|---|\n" + string(src))
		}
		before := liveHeap()
		doc := md.Parse(src, nil)
		held := liveHeap() - before
		nodes := int64(0)
		ast.WalkFunc(doc, func(_ ast.Node, entering bool) ast.WalkStatus {
			if entering {
				nodes++
			}
			return ast.GoToNext
		})
		if per := held / nodes; per > astNodeCost {
			t.Errorf("%s: a node holds %d bytes, past astNodeCost, %d", name, per, astNodeCost)
		}
		runtime.KeepAlive(src)
		runtime.KeepAlive(doc)
	}
}

func TestRender_ChargesTheTreeThenTheBody(t *testing.T) {
	src := []byte(strings.Repeat("- a\n", 1000))
	ctx, mem, end := limits.WithBudget(context.Background(), limits.Limits{MaxConvertMemoryBytes: 1 << 30}.WithDefaults())
	defer end()
	body, _, err := render(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	if mem.Held() != int64(len(body)) {
		t.Errorf("render holds %d bytes charged, want the body's %d", mem.Held(), len(body))
	}
	if mem.Peak() < 2000*astNodeCost {
		t.Errorf("render peaked at %d bytes, under the tree of 3,000 nodes", mem.Peak())
	}
	ctx, _, end2 := limits.WithBudget(context.Background(), limits.Limits{MaxConvertMemoryBytes: 1000 * astNodeCost}.WithDefaults())
	defer end2()
	if _, _, err := render(ctx, src); !errors.Is(err, limits.ErrMemoryBudget) {
		t.Errorf("render under a budget smaller than its tree = %v, want ErrMemoryBudget", err)
	}
}
