package fontsubset

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// maxDirectDepth caps how deep the census descends through direct
// dictionaries and arrays inside one object. Anti-DoS guard, not
// spec-mandated: real documents nest direct objects a handful of levels deep
// (a page's /Resources holding /Font holding a font dictionary is three), and
// a census that stops short cannot prove it saw every reference, so it gives
// up on the whole pass rather than subset a font it may have missed a use of.
const maxDirectDepth = 64

// maxRefChain caps how many references [graph.deref] follows when a reference
// names an object that is itself a reference. Anti-DoS guard, not
// spec-mandated: ISO 32000-1, 7.3.10 makes an indirect object's value a
// direct object, so a chain is already malformed, and two links cover every
// producer seen.
const maxRefChain = 2

// pathSep separates the keys of a [loc] path. A PDF name never holds a NUL
// byte (ISO 32000-1, 7.3.5), and array indexes are decimal, so no key holds
// it.
const pathSep = "\x00"

// trailerObj is the object number [loc] gives the trailer dictionary, which
// is no object of the allocator.
const trailerObj = -1

// loc places a value in the object graph: the object it sits in and the keys
// and array indexes leading to it from that object's value, empty for the
// object itself. A dictionary or array has one loc, the one it is written at,
// so two routes reaching it through the same reference agree on it.
type loc struct {
	obj  int
	path string
}

// slot returns the location of the entry key of the value at l.
func (l loc) slot(key string) loc {
	if l.path == "" {
		return loc{obj: l.obj, path: key}
	}
	return loc{obj: l.obj, path: l.path + pathSep + key}
}

// split returns the value l is an entry of and the entry's key. l must not
// be an object's own location.
func (l loc) split() (loc, string) {
	i := strings.LastIndex(l.path, pathSep)
	if i < 0 {
		return loc{obj: l.obj}, l.path
	}
	return loc{obj: l.obj, path: l.path[:i]}, l.path[i+len(pathSep):]
}

// node is a value of the object graph and where it sits.
type node struct {
	val ast.PdfObject
	at  loc
}

// dict returns the node's dictionary: the value itself, or a stream's
// dictionary, nil for anything else.
func (n node) dict() ast.PdfDict {
	switch v := n.val.(type) {
	case ast.PdfDict:
		return v
	case ast.PdfStream:
		return v.Dict
	}
	return nil
}

// stream returns the node's stream and whether it is one.
func (n node) stream() (ast.PdfStream, bool) {
	s, ok := n.val.(ast.PdfStream)
	return s, ok
}

// name returns the dictionary entry key as a name, "" when it is not one.
func (n node) name(key ast.PdfName) ast.PdfName {
	v, _ := n.dict()[key].(ast.PdfName)
	return v
}

// graph is the object graph a rewrite writes: the allocator's pending
// objects and the trailer, with every place a reference to each object is
// written.
type graph struct {
	pending map[int]ast.PdfObject
	trailer ast.PdfDict
	// refs holds, for each object number, the slot of every reference to it:
	// the location of the dictionary entry or array element holding it.
	refs map[int][]loc
	// daNames holds every font name a default appearance string names (ISO
	// 32000-1, 12.7.3.3, Table 218 and 12.5.6.6, Table 174), wherever /DA
	// appears.
	daNames map[ast.PdfName]bool
	// signed reports a dictionary holding /ByteRange, a signature's value
	// (ISO 32000-1, 12.8.1, Table 252).
	signed bool
	// incomplete reports a census that could not read every value: one
	// nested past [maxDirectDepth], or a /DA string it could not parse.
	incomplete string
}

// newGraph takes the census of the pending objects and the trailer.
func newGraph(ctx context.Context, pending map[int]ast.PdfObject, trailer ast.PdfDict) (*graph, error) {
	g := &graph{pending: pending, trailer: trailer, refs: make(map[int][]loc), daNames: make(map[ast.PdfName]bool)}
	g.scan(ctx, trailer, loc{obj: trailerObj}, 0)
	i := 0
	for num, obj := range pending {
		if i%ctxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("taking the census of the objects: %w", err)
			}
		}
		i++
		g.scan(ctx, obj, loc{obj: num}, 0)
	}
	return g, nil
}

// scan records every reference v holds, and every /DA and /ByteRange entry.
func (g *graph) scan(ctx context.Context, v ast.PdfObject, at loc, depth int) {
	if depth > maxDirectDepth {
		g.incomplete = fmt.Sprintf("a value nested more than %d levels deep", maxDirectDepth)
		return
	}
	switch v := v.(type) {
	case ast.PdfIndirectRef:
		g.refs[v.ObjectNum] = append(g.refs[v.ObjectNum], at)
	case ast.PdfStream:
		g.scan(ctx, v.Dict, at, depth)
	case ast.PdfDict:
		if _, ok := v["ByteRange"]; ok {
			g.signed = true
		}
		if da, ok := v["DA"].(ast.PdfString); ok {
			g.readDA(ctx, da)
		}
		for key, elem := range v {
			g.scan(ctx, elem, at.slot(string(key)), depth+1)
		}
	case ast.PdfArray:
		for i, elem := range v {
			g.scan(ctx, elem, at.slot(strconv.Itoa(i)), depth+1)
		}
	}
}

// readDA records the font names a default appearance string selects with Tf
// (ISO 32000-1, 12.7.3.3). Every name operand counts, whatever its operator,
// so a string that reads in an unexpected way still names its font.
func (g *graph) readDA(ctx context.Context, da ast.PdfString) {
	ops, err := content.ParseStreamData(content.WithWholeStreams(ctx), []byte(da), content.SplitGluedOperators())
	if err != nil {
		g.incomplete = "a default appearance string that does not parse"
		return
	}
	for _, op := range ops {
		for _, operand := range op.Operands {
			if name, ok := operand.(ast.PdfName); ok {
				g.daNames[name] = true
			}
		}
	}
}

// object returns the pending object num as a node.
func (g *graph) object(num int) node {
	return g.deref(ast.PdfIndirectRef{ObjectNum: num}, loc{obj: num})
}

// root returns the trailer as a node.
func (g *graph) root() node {
	return node{val: g.trailer, at: loc{obj: trailerObj}}
}

// deref returns v, written at at, as a node: the object a reference names,
// at its own location, or v itself. A reference to nothing, or to another
// reference past [maxRefChain], is null.
func (g *graph) deref(v ast.PdfObject, at loc) node {
	for range maxRefChain + 1 {
		ref, ok := v.(ast.PdfIndirectRef)
		if !ok {
			return node{val: v, at: at}
		}
		target, ok := g.pending[ref.ObjectNum]
		if !ok {
			return node{val: ast.PdfNull{}, at: loc{obj: ref.ObjectNum}}
		}
		v, at = target, loc{obj: ref.ObjectNum}
	}
	return node{val: ast.PdfNull{}, at: at}
}

// child returns the entry key of a dictionary or stream node, or the element
// at index key of an array node, and false when there is none.
func (g *graph) child(n node, key string) (node, bool) {
	var v ast.PdfObject
	switch c := n.val.(type) {
	case ast.PdfArray:
		i, err := strconv.Atoi(key)
		if err != nil || i < 0 || i >= len(c) {
			return node{}, false
		}
		v = c[i]
	default:
		d := n.dict()
		if d == nil {
			return node{}, false
		}
		var ok bool
		if v, ok = d[ast.PdfName(key)]; !ok {
			return node{}, false
		}
	}
	got := g.deref(v, n.at.slot(key))
	if _, null := got.val.(ast.PdfNull); null || got.val == nil {
		return node{}, false
	}
	return got, true
}

// entries returns the entries of a dictionary node as nodes, keyed by name.
func (g *graph) entries(n node) map[ast.PdfName]node {
	d, ok := n.val.(ast.PdfDict)
	if !ok {
		return nil
	}
	out := make(map[ast.PdfName]node, len(d))
	for key := range d {
		if c, ok := g.child(n, string(key)); ok {
			out[key] = c
		}
	}
	return out
}

// elements returns the elements of an array node as nodes.
func (g *graph) elements(n node) []node {
	a, ok := n.val.(ast.PdfArray)
	if !ok {
		return nil
	}
	out := make([]node, 0, len(a))
	for i := range a {
		if c, ok := g.child(n, strconv.Itoa(i)); ok {
			out = append(out, c)
		}
	}
	return out
}

// holder is a slot holding a value: the value whose entry it is, and the
// entry's key.
type holder struct {
	container loc
	key       string
}

// holders returns every slot holding the value at l: each slot holding a
// reference to it when it is an object of its own, and the one slot it is
// written in otherwise.
func (g *graph) holders(l loc) []holder {
	if l.path != "" {
		c, key := l.split()
		return []holder{{container: c, key: key}}
	}
	slots := g.refs[l.obj]
	out := make([]holder, 0, len(slots))
	for _, s := range slots {
		c, key := s.split()
		out = append(out, holder{container: c, key: key})
	}
	return out
}
