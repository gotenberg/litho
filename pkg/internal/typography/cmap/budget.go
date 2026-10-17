package cmap

import (
	"context"
	"fmt"
	"math"
)

// maxCMapRangeCodes caps the codes the range entries of one CMap program
// expand to, across all its bfrange (string destination) and cidrange entries.
// Anti-DoS guard, not spec-mandated: one entry is about 20 bytes and expands to
// up to [maxRangeEntries] codes, so without a total a crafted stream maps
// billions of codes per megabyte. A code space of two-byte codes holds 65536
// codes, and the cap allows four times that, the bound [maxBCMapRangeCodes]
// puts on the binary CMaps litho bundles. A program past it fails to parse
// with [ErrCMapTooLarge]. Entries mapping one code each (bfchar, cidchar, a
// bfrange array) cost their own input bytes and are bounded by the stream
// budget instead.
//
// # Corpus evidence
//
//   - All corpora: 5498 /ToUnicode, /Encoding and /UseCMap streams, the
//     largest expanding to 65536 codes through bfrange and 65536 through
//     cidrange (41 streams reach that, each with one range of 65536 codes), a
//     margin of four.
const maxCMapRangeCodes = maxBCMapRangeCodes

// maxCodeSpaceRanges caps the codespace ranges one CMap program declares.
// Anti-DoS guard, not spec-mandated: every code a content stream shows is
// tested against each range in turn, so the count multiplies the cost of
// every shown byte. A program past it fails to parse with [ErrCMapTooLarge].
//
// # Corpus evidence
//
//   - All corpora: of 5498 CMap streams, none declares more than 7 codespace
//     ranges and 5419 declare one.
const maxCodeSpaceRanges = 256

// maxCodeBytes is the widest character code a CMap defines: ISO 32000-1,
// 9.7.6.2 reads a code of one to four bytes. [ParseCID] keys codes by integer,
// so a wider code, which no codespace a reader matches against produces, would
// overflow the integer instead.
//
// # Corpus evidence
//
//   - All corpora: no cidchar, cidrange, notdef or codespace entry of the 5498
//     CMap streams writes a code wider than four bytes.
const maxCodeBytes = 4

// ctxCheckInterval is how many block entries or expanded codes a CMap parse
// handles between two context checks.
const ctxCheckInterval = 1024

// parseBudget tracks what one CMap program has cost so far against its caps.
// The zero value is unusable; build one with [newParseBudget].
type parseBudget struct {
	rangeCodes, maxRangeCodes int
	codeSpaces, maxCodeSpaces int
	steps                     int
}

// newParseBudget returns the budget one CMap program parses under.
func newParseBudget() *parseBudget {
	return &parseBudget{maxRangeCodes: maxCMapRangeCodes, maxCodeSpaces: maxCodeSpaceRanges}
}

// step counts one unit of work and checks ctx every [ctxCheckInterval] units.
func (b *parseBudget) step(ctx context.Context) error {
	b.steps++
	if b.steps%ctxCheckInterval == 0 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("parsing CMap: %w", err)
		}
	}
	return nil
}

// rangeCode counts one code a range entry expands to.
func (b *parseBudget) rangeCode(ctx context.Context) error {
	b.rangeCodes++
	if b.rangeCodes > b.maxRangeCodes {
		return fmt.Errorf("range entries expand past %d codes: %w", b.maxRangeCodes, ErrCMapTooLarge)
	}
	return b.step(ctx)
}

// codeSpace counts one codespace range.
func (b *parseBudget) codeSpace() error {
	b.codeSpaces++
	if b.codeSpaces > b.maxCodeSpaces {
		return fmt.Errorf("more than %d codespace ranges: %w", b.maxCodeSpaces, ErrCMapTooLarge)
	}
	return nil
}

// codeInt reads a character code as the integer [ParseCID] keys it by. It
// reports false for a code outside the one to four bytes ISO 32000-1, 9.7.6.2
// defines, whose value would not fit the key.
func codeInt(s []byte) (int, bool) {
	if len(s) == 0 || len(s) > maxCodeBytes {
		return 0, false
	}
	v := 0
	for _, b := range s {
		v = v<<8 | int(b)
	}
	return v, true
}

// cidValue reads the CID a cidchar, cidrange or notdef entry maps its first
// code to. It reports false for a negative CID, which names no glyph (CIDs
// count up from 0, Adobe Technical Note #5014), and for one that span more
// codes would carry past the largest integer.
func cidValue(d int64, span int) (int, bool) {
	if d < 0 || d > int64(math.MaxInt)-int64(span) {
		return 0, false
	}
	return int(d), true
}
