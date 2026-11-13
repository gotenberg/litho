package css

import (
	"strconv"
	"strings"
)

// maxLayers caps the cascade layers one stylesheet names, its imports
// included: 4096. Anti-DoS guard, not spec-mandated: CSS Cascade 5, 6.4
// sets no bound, and layered frameworks name a dozen. A layer past the cap
// is read as unlayered.
const maxLayers = 4096

// maxLayerDepth caps the names of one layer path (a.b.c holds three): 32.
// Anti-DoS guard, not spec-mandated: it bounds the walk of the layer tree,
// and real sheets nest two or three deep. A deeper name makes its rule
// invalid, and a layer nested deeper through blocks is read as unlayered.
const maxLayerDepth = 32

// numOrigins counts the origins, each with its own layer order.
const numOrigins = int(Author) + 1

// originSlot returns the index of o's layer order; an origin past Author
// cascades as Author.
func originSlot(o Origin) int { return int(min(o, Author)) }

// unlayered is the layer index of a rule outside every cascade layer.
const unlayered = -1

// layerName parses a <layer-name> (CSS Cascade 5, 6.4.2): identifiers
// joined by "." with no whitespace, none of them a CSS-wide keyword.
func layerName(l []token) ([]string, bool) {
	var out []string
	for i, t := range l {
		if i%2 == 1 {
			if !t.isDelim('.') {
				return nil, false
			}
			continue
		}
		if t.typ != tIdent {
			return nil, false
		}
		if _, wide := wideKeyword([]token{t}); wide || eqFold(t.value, "default") {
			return nil, false
		}
		out = append(out, t.value)
	}
	if len(out) == 0 || len(l)%2 == 0 || len(out) > maxLayerDepth {
		return nil, false
	}
	return out, true
}

// childLayer returns the index of the layer named names inside the
// current one, declaring it when it is new; a nil names is a new
// anonymous layer (CSS Cascade 5, 6.4.2). The path of an anonymous layer
// holds a segment that starts with "\x00", which no identifier does.
func (sp *sheetParser) childLayer(names []string) int {
	path := ""
	if sp.layer != unlayered {
		path = sp.sheet.layers[sp.layer] + "."
	}
	if names == nil {
		sp.anon++
		path += "\x00" + strconv.Itoa(sp.anon)
	} else {
		path += strings.Join(names, ".")
	}
	if strings.Count(path, ".") >= maxLayerDepth {
		return unlayered
	}
	if i, ok := sp.layerIndex[path]; ok {
		return i
	}
	if len(sp.sheet.layers) >= maxLayers {
		return unlayered
	}
	if sp.layerIndex == nil {
		sp.layerIndex = map[string]int{}
	}
	i := len(sp.sheet.layers)
	sp.sheet.layers = append(sp.sheet.layers, path)
	sp.layerIndex[path] = i
	return i
}

// enterLayer runs inner, which parses the rule list of a layer block
// rule whose prelude is prelude, within the layer it names, and reports
// false for an invalid prelude, which drops the rule.
func (sp *sheetParser) enterLayer(prelude []token, inner func()) bool {
	var names []string
	if l := significant(prelude); len(l) > 0 {
		var ok bool
		if names, ok = layerName(l); !ok {
			return false
		}
	}
	outer := sp.layer
	sp.layer = sp.childLayer(names)
	inner()
	sp.layer = outer
	return true
}

// declareLayers handles the statement form of @layer, a list of layer
// names that fixes their order before any rule of theirs (CSS Cascade 5,
// 6.4.2). An invalid list declares nothing.
func (sp *sheetParser) declareLayers(prelude []token) {
	var all [][]string
	for _, part := range splitCommas(significant(prelude)) {
		names, ok := layerName(trimWS(part))
		if !ok {
			return
		}
		all = append(all, names)
	}
	for _, names := range all {
		sp.childLayer(names)
	}
}

// layerTree orders the cascade layers of one origin (CSS Cascade 5,
// 6.4.3): layers in the order their names first appear, a layer's
// sublayers before its own rules, every layer before the rules outside
// any.
type layerTree struct {
	children map[string][]string
	pos      map[string]int
}

// add declares the layer path, then each of its ancestors first.
func (t *layerTree) add(path string) {
	if t.children == nil {
		t.children = map[string][]string{}
	}
	parent := ""
	for i := 0; ; i++ {
		dot := strings.IndexByte(path[i:], '.')
		end := len(path)
		if dot >= 0 {
			end = i + dot
		}
		p := path[:end]
		if _, seen := t.children[p]; !seen {
			t.children[p] = nil
			t.children[parent] = append(t.children[parent], p)
		}
		if dot < 0 {
			return
		}
		parent, i = p, end
	}
}

// number gives each layer its position, the root (the unlayered rules)
// the last, and returns the root's.
func (t *layerTree) number() int {
	t.pos = map[string]int{}
	n := 0
	var walk func(p string, depth int)
	walk = func(p string, depth int) {
		if depth <= maxLayerDepth {
			for _, c := range t.children[p] {
				walk(c, depth+1)
			}
		}
		t.pos[p] = n
		n++
	}
	walk("", 0)
	return t.pos[""]
}

// sheetLayers maps the layer indexes of each sheet to positions in the
// layer order of the sheet's origin; top is the position of the unlayered
// rules of each origin, the highest.
type sheetLayers struct {
	pos [][]int
	top [numOrigins]int
}

// orderLayers orders the cascade layers of sheets, one order per origin.
// The anonymous layers of two sheets are distinct.
func orderLayers(sheets []Sheet) sheetLayers {
	var trees [numOrigins]layerTree
	paths := make([][]string, len(sheets))
	for si, sh := range sheets {
		if sh.Sheet == nil || len(sh.Sheet.layers) == 0 {
			continue
		}
		paths[si] = make([]string, len(sh.Sheet.layers))
		for i, p := range sh.Sheet.layers {
			if strings.Contains(p, "\x00") {
				p = strings.ReplaceAll(p, "\x00", "\x00"+strconv.Itoa(si)+":")
			}
			paths[si][i] = p
			trees[originSlot(sh.Origin)].add(p)
		}
	}
	var out sheetLayers
	for o := range trees {
		out.top[o] = trees[o].number()
	}
	out.pos = make([][]int, len(sheets))
	for si, sh := range sheets {
		if paths[si] == nil {
			continue
		}
		out.pos[si] = make([]int, len(paths[si]))
		for i, p := range paths[si] {
			out.pos[si][i] = trees[originSlot(sh.Origin)].pos[p]
		}
	}
	return out
}

// of returns the position of layer index i of sheet si, top for an
// unlayered rule.
func (l *sheetLayers) of(si int, o Origin, i int) int {
	if i == unlayered || i >= len(l.pos[si]) {
		return l.top[originSlot(o)]
	}
	return l.pos[si][i]
}

// layerRank orders a declaration of a rule at layer position pos among
// those of its origin and importance (CSS Cascade 5, 6.4.3): a later
// layer wins for normal declarations, an earlier one for important ones.
// The rules outside any layer have the highest position.
func layerRank(pos int, important bool) int {
	if important {
		return -pos
	}
	return pos
}

// attachedRank is the layer rank of the declarations of a style
// attribute, which win over every rule of their origin and importance
// whatever its layer (CSS Cascade 5, 6.1, element-attached styles).
const attachedRank = 1 << 30
