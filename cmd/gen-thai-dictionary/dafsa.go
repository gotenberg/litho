package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// The layout of a packed edge, as pkg/internal/dictbreak reads it: four
// bytes, the label in the low seven bits of the first and the last-edge
// flag in its high bit, the final flag in the high bit of the second and
// a 23-bit target in the rest.
const (
	edgeSize = 4
	maxLabel = 0x7f
	maxEdges = 1 << 23
)

// errBadWord reports a word encode cannot hold: empty, or holding a
// letter outside the 127 letters above base.
var errBadWord = errors.New("word outside the dictionary's letters")

// errTooLarge reports a word list whose automaton has more edges than a
// target can address.
var errTooLarge = errors.New("dictionary too large")

// encode packs words, in any order and with repeats, into the edges of
// a dictbreak.Dictionary whose labels count from base. It returns [errBadWord]
// for an empty word or one holding a letter at or below base or more
// than 127 above it, and [errTooLarge] past 2^23 edges.
func encode(words []string, base rune) (string, error) {
	list := make([][]byte, 0, len(words))
	for _, w := range words {
		if w == "" {
			return "", fmt.Errorf("%w: empty word", errBadWord)
		}
		lw := make([]byte, 0, len(w))
		for _, r := range w {
			l := r - base
			if l <= 0 || l > maxLabel {
				return "", fmt.Errorf("%w: %q", errBadWord, w)
			}
			lw = append(lw, byte(l))
		}
		list = append(list, lw)
	}
	slices.SortFunc(list, func(a, b []byte) int { return strings.Compare(string(a), string(b)) })
	list = slices.CompactFunc(list, func(a, b []byte) bool { return string(a) == string(b) })
	b := newBuilder()
	for _, w := range list {
		b.add(w)
	}
	root := b.finish()
	return b.pack(root)
}

// node is a state of the automaton while it is built.
type node struct {
	final  bool
	labels []byte
	next   []*node
	// id is the node's number in the register, 0 before it is registered.
	id int
}

// builder builds the minimal automaton of a sorted word list by
// registering each state once its words are all read and merging it with
// an equal state registered before (Daciuk, Mihov, Watson and Watson,
// "Incremental construction of minimal acyclic finite-state automata",
// Computational Linguistics 26(1), 2000, 3).
type builder struct {
	root     *node
	register map[string]*node
	// path holds the states along the last word added, the root first.
	path []*node
}

func newBuilder() *builder {
	root := &node{}
	return &builder{root: root, register: map[string]*node{}, path: []*node{root}}
}

// add adds w, which sorts after every word added before it.
func (b *builder) add(w []byte) {
	common := 0
	for common < len(w) && common+1 < len(b.path) {
		n := b.path[common]
		if len(n.labels) == 0 || n.labels[len(n.labels)-1] != w[common] {
			break
		}
		common++
	}
	b.minimize(common)
	n := b.path[common]
	for _, l := range w[common:] {
		c := &node{}
		n.labels = append(n.labels, l)
		n.next = append(n.next, c)
		b.path = append(b.path, c)
		n = c
	}
	n.final = true
}

// minimize registers the states of the last word deeper than depth,
// the deepest first, each replaced by an equal registered state.
func (b *builder) minimize(depth int) {
	for i := len(b.path) - 1; i > depth; i-- {
		parent := b.path[i-1]
		child := parent.next[len(parent.next)-1]
		key := child.key()
		if same, ok := b.register[key]; ok {
			parent.next[len(parent.next)-1] = same
		} else {
			child.id = len(b.register) + 1
			b.register[key] = child
		}
	}
	b.path = b.path[:depth+1]
}

// finish registers what remains of the last word and returns the root.
func (b *builder) finish() *node {
	b.minimize(0)
	return b.root
}

// key is what two equal states share: finality, and each edge's label
// and target.
func (n *node) key() string {
	var sb strings.Builder
	if n.final {
		sb.WriteByte('F')
	}
	for i, l := range n.labels {
		sb.WriteByte(l)
		sb.WriteString(strconv.Itoa(n.next[i].id))
		sb.WriteByte(',')
	}
	return sb.String()
}

// pack lays the states out from root, depth first, each state's edges
// side by side, and writes the edges.
func (b *builder) pack(root *node) (string, error) {
	first := map[*node]int{}
	var order []*node
	count := 0
	var place func(n *node)
	place = func(n *node) {
		if _, ok := first[n]; ok || len(n.labels) == 0 {
			return
		}
		first[n] = count
		count += len(n.labels)
		order = append(order, n)
		for _, c := range n.next {
			place(c)
		}
	}
	place(root)
	if count >= maxEdges {
		return "", fmt.Errorf("%w: %d edges, at most %d", errTooLarge, count, maxEdges-1)
	}
	out := make([]byte, 0, count*edgeSize)
	for _, n := range order {
		for i, l := range n.labels {
			c := n.next[i]
			b0 := l
			if i == len(n.labels)-1 {
				b0 |= 0x80
			}
			t := first[c] // 0 for a state with no edges
			b1 := byte(t >> 16 & 0x7f)
			if c.final {
				b1 |= 0x80
			}
			out = append(out, b0, b1, byte(t>>8&0xff), byte(t&0xff))
		}
	}
	return string(out), nil
}
