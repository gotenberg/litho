package css

import "hash/maphash"

// maxReusedCascades bounds the cascades one Compute call remembers for
// reuse; past it the cascade of a new combination is computed and not
// remembered. Anti-DoS guard, not spec-mandated: an entry holds the
// declarations of one element, a few hundred bytes, and a document's
// elements fall in a few hundred combinations of parent style and
// matching rules, so the bound keeps the memory under a few MiB whatever
// the document.
const maxReusedCascades = 1 << 12

// reuseKey is what decides an element's computed style besides its
// declarations: the computed style of its parent, the root's, and the
// element's place among display: contents boxes. hash sums the
// declarations in cascade order.
type reuseKey struct {
	parent, root *Style
	isRoot       bool
	noContents   bool
	boxParent    Display
	hash         uint64
}

// reuseDecl is one declaration of a remembered cascade and the origin
// and importance it competed with.
type reuseDecl struct {
	d    *Declaration
	rank int
}

// reusedCascade is a remembered cascade: its declarations in cascade
// order and the style they computed.
type reusedCascade struct {
	decls []reuseDecl
	st    *Style
}

// reuseCache remembers the computed styles of the cascades of one
// Compute call. Two elements whose declarations are the same in the same
// order, under parents of the same computed style, compute the same
// style (CSS Cascade 4, 4: the computed value is a function of the
// cascaded value and the parent's computed values), so the second takes
// the first's without computing it again. The rows of a table, the
// paragraphs of a section and the items of a list are such elements.
type reuseCache struct {
	// on is set while Compute walks the tree, whose parent styles are
	// the shared ones it computed.
	on      bool
	seed    maphash.Seed
	byKey   map[reuseKey][]reusedCascade
	entries int
	// hits counts the cascades that reused a remembered style.
	hits int
}

// reusable reports whether the cascade of cands, in cascade order, can
// be reused: Compute walks the tree, so the parent's style is one a
// shared table holds and nobody writes, and the computed values depend on
// nothing but the declarations and the key. A custom property, a var()
// or attr() value (which reads the element's attributes), an animation
// (which reads the element's ancestors), a presentational hint or a style
// attribute (parsed again for each element, never equal to another's)
// and a restricted set of longhands (::first-line) keep it out.
func (cs *computeState) reusable(cands []cand, only *[numProps]bool) bool {
	if !cs.reuse.on || cs.styles == nil || only != nil {
		return false
	}
	for _, c := range cands {
		if c.ord < 0 || c.spec == styleAttrSpec || c.d.isCustom || len(c.d.pending) > 0 {
			return false
		}
		for _, e := range c.d.entries {
			if e.id == pAnimationName {
				return false
			}
		}
	}
	return true
}

// reuseKeyOf returns the key of the cascade of cands under parent.
func (cs *computeState) reuseKeyOf(cands []cand, parent *Style, isRoot bool) reuseKey {
	c := &cs.reuse
	if c.byKey == nil {
		c.seed = maphash.MakeSeed()
		c.byKey = map[reuseKey][]reusedCascade{}
	}
	var h maphash.Hash
	h.SetSeed(c.seed)
	for _, x := range cands {
		maphash.WriteComparable(&h, x.d)
		maphash.WriteComparable(&h, x.rank)
	}
	return reuseKey{parent: parent, root: cs.root, isRoot: isRoot, noContents: cs.noContents, boxParent: cs.boxParent, hash: h.Sum64()}
}

// reused returns the style a remembered cascade of cands under k
// computed, nil for none.
func (cs *computeState) reused(k reuseKey, cands []cand) *Style {
	for _, r := range cs.reuse.byKey[k] {
		if sameDecls(r.decls, cands) {
			cs.reuse.hits++
			return r.st
		}
	}
	return nil
}

// remember keeps st as the style the cascade of cands under k computes,
// while fewer than [maxReusedCascades] are kept.
func (cs *computeState) remember(k reuseKey, cands []cand, st *Style) {
	c := &cs.reuse
	if c.entries >= maxReusedCascades {
		return
	}
	decls := make([]reuseDecl, len(cands))
	for i, x := range cands {
		decls[i] = reuseDecl{d: x.d, rank: x.rank}
	}
	c.byKey[k] = append(c.byKey[k], reusedCascade{decls: decls, st: st})
	c.entries++
}

// sameDecls reports whether decls and cands hold the same declarations
// with the same ranks in the same order.
func sameDecls(decls []reuseDecl, cands []cand) bool {
	if len(decls) != len(cands) {
		return false
	}
	for i, x := range cands {
		if decls[i].d != x.d || decls[i].rank != x.rank {
			return false
		}
	}
	return true
}
