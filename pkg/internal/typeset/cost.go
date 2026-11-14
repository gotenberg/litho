package typeset

// The costs below are what the fragments of a page hold on the heap, which
// [ContentCost] adds up. Each counts the fragment's struct in its
// allocation size class and its slot in the slice that holds it; what a
// fragment points to that its page shares with others (a [Face], an
// [ImageSource], a [StructElem]) is charged where it is made.
//
// # Corpus evidence
//
//   - probe, html: 2,000 boxes of a gradient tiled every 10 px paint 16,000
//     shaded paths of five operators, 530 bytes each with their shading;
//     20,000 bordered paragraphs paint 20,000 rings of ten operators, 664
//     bytes each, and 20,000 runs of one word, 355 bytes each with their
//     glyphs.
const (
	// fragmentSlotCost is the interface value that holds a fragment in its
	// page's or its group's content.
	fragmentSlotCost = 16
	// pathCost is a [Path] and its [Fill], 56 and 48 bytes on a 64-bit
	// platform in allocations of 64 and 48.
	pathCost = 112
	// pathOpCost is one [PathOp], 56 bytes, with the slack of its slice's
	// size class.
	pathOpCost = 64
	// strokeCost is a [Stroke], 88 bytes in an allocation of 96.
	strokeCost = 96
	// dashCost is one entry of a [Stroke]'s dash array.
	dashCost = 8
	// shadingCost is a [Shading], 128 bytes, copied for each placement of
	// a gradient.
	shadingCost = 128
	// stopCost is one [ColorStop], 40 bytes in a slice whose size class
	// rounds it up.
	stopCost = 48
	// textCost is a [Text] run, 160 bytes, its glyphs aside:
	// [limits.GlyphCost] charges them as the text stack lays them out.
	textCost = 160
	// colorCost is one entry of a [Text]'s palette.
	colorCost = 32
	// imageCost is an [Image], 56 bytes in an allocation of 64, with slack.
	imageCost = 72
	// groupCost is a [Group], 104 bytes in an allocation of 112.
	groupCost = 112
	// transparencyCost is a [TransparencyGroup] and its [SoftMask], 16
	// and 32 bytes.
	transparencyCost = 48
)

// ContentCost returns what content holds on the heap, nested groups and
// soft masks included, the glyphs of its runs aside ([limits.GlyphCost]),
// as the costs above count it. A converter that keeps pages charges it to
// the conversion's budget while it holds them. Groups nested past
// [maxGroupDepth] are not counted: Validate refuses them.
func ContentCost(content []Fragment) int64 {
	type item struct {
		frags []Fragment
		depth int
	}
	var n int64
	stack := []item{{frags: content}}
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, f := range it.frags {
			n += fragmentSlotCost
			g, ok := f.(*Group)
			if !ok {
				n += leafHeld(f)
				continue
			}
			if g == nil {
				continue
			}
			n += groupCost + pathHeld(g.Clip)
			if it.depth >= maxGroupDepth {
				continue
			}
			stack = append(stack, item{frags: g.Content, depth: it.depth + 1})
			if tg := g.Transparency; tg != nil {
				n += transparencyCost
				if tg.Mask != nil {
					stack = append(stack, item{frags: tg.Mask.Content, depth: it.depth + 1})
				}
			}
		}
	}
	return n
}

// FragmentCost returns what the fragment f holds on the heap, its slot in
// the content that holds it included, as [ContentCost] counts it.
func FragmentCost(f Fragment) int64 {
	if g, ok := f.(*Group); ok && g != nil {
		return ContentCost([]Fragment{g})
	}
	return fragmentSlotCost + leafHeld(f)
}

// leafHeld returns what the fragment f, not a group, holds besides its
// slot; 0 for a nil one.
func leafHeld(f Fragment) int64 {
	switch f := f.(type) {
	case *Text:
		if f != nil {
			return textCost + int64(len(f.Palette))*colorCost
		}
	case *Image:
		if f != nil {
			return imageCost
		}
	case *Path:
		return pathHeld(f)
	}
	return 0
}

// pathHeld returns what the path p holds, 0 for nil.
func pathHeld(p *Path) int64 {
	if p == nil {
		return 0
	}
	n := pathCost + int64(len(p.Ops))*pathOpCost
	if p.Fill != nil && p.Fill.Shading != nil {
		n += shadingCost + int64(len(p.Fill.Shading.Stops))*stopCost
	}
	if p.Stroke != nil {
		n += strokeCost + int64(len(p.Stroke.Dash))*dashCost
	}
	return n
}
