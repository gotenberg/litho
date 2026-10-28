package jbig2

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/filter/mq"
	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// maxSymbols bounds how many symbols a dictionary may define or export and how
// many a text region may address. It is an anti-DoS guard, not spec-mandated:
// the counts are 32-bit fields, and IAID needs 2^(n+1) contexts for n-bit
// symbol IDs. A dictionary shared by a whole scanned book holds a few
// thousand symbols; 2^20 leaves a wide margin, and a dictionary's input and
// new symbols together then need at most 21-bit IDs, 8 MB of IAID contexts.
const maxSymbols = 1 << 20

// symbolDict is what a decoded symbol dictionary segment leaves for the
// segments that refer to it: its exported symbols and, when its header asked
// for them to be retained, the generic and refinement contexts it ended with
// (ITU-T T.88, E.3.8).
type symbolDict struct {
	exported []*Bitmap
	retained *retainedContexts
}

// retainedContexts are the bitmap coding statistics one symbol dictionary
// hands to a later one, with the parameters T.88, 7.4.2.2 step 3 requires the
// later dictionary to share.
type retainedContexts struct {
	gb, gr      []mq.Context
	template    int
	refTemplate int
	refagg      bool
	at          [4][2]int
	refAT       [2][2]int
}

// symbolHeader is the symbol dictionary segment data header of ITU-T T.88,
// 7.4.2.1.
type symbolHeader struct {
	flags         uint16
	huffman       bool
	refagg        bool
	contextUsed   bool
	contextRetain bool
	template      int
	refTemplate   int
	at            [4][2]int
	refAT         [2][2]int
	numExported   int64
	numNew        int64
	dataStart     int
}

func parseSymbolHeader(data []byte) (symbolHeader, error) {
	c := &cursor{b: data}
	flags := c.u16()
	h := symbolHeader{
		flags:         flags,
		huffman:       flags&1 != 0,
		refagg:        flags&2 != 0,
		contextUsed:   flags&0x100 != 0,
		contextRetain: flags&0x200 != 0,
		template:      int(flags >> 10 & 3),
		refTemplate:   int(flags >> 12 & 1),
	}
	if !h.huffman {
		for i := range atCount(h.template) {
			h.at[i][0] = c.s8()
			h.at[i][1] = c.s8()
		}
	}
	if h.refagg && h.refTemplate == 0 {
		for i := range 2 {
			h.refAT[i][0] = c.s8()
			h.refAT[i][1] = c.s8()
		}
	}
	h.numExported = int64(c.u32())
	h.numNew = int64(c.u32())
	if c.err != nil {
		return symbolHeader{}, c.err
	}
	h.dataStart = c.off
	return h, nil
}

// decodeSymbolDict decodes a symbol dictionary segment, ITU-T T.88, 7.4.2.2,
// whose input symbols are inSyms, whose last referred-to dictionary, if any,
// is last, and whose referred-to tables segments supply tables.
func decodeSymbolDict(ctx context.Context, seg segment, inSyms []*Bitmap, last *symbolDict, tables []*huffTable, b *budget) (*symbolDict, error) {
	h, err := parseSymbolHeader(seg.data)
	if err != nil {
		return nil, err
	}
	if h.numNew > maxSymbols || h.numExported > maxSymbols || int64(len(inSyms)) > maxSymbols {
		return nil, fmt.Errorf("%w: symbol dictionary of %d new and %d exported symbols over %d input symbols exceeds %d",
			ErrLimit, h.numNew, h.numExported, len(inSyms), maxSymbols)
	}

	st := &arithState{dec: mq.NewDecoder(seg.data[h.dataStart:]), budget: b}
	// Steps 3 and 4: bitmap statistics start from the last referred-to
	// dictionary's when this one uses them, and from reset otherwise.
	if h.contextUsed {
		if last == nil || last.retained == nil {
			return nil, fmt.Errorf("%w: symbol dictionary uses retained contexts that no referred-to dictionary kept", ErrMalformed)
		}
		r := last.retained
		if r.template != h.template || r.refTemplate != h.refTemplate || r.refagg != h.refagg || r.at != h.at || r.refAT != h.refAT {
			return nil, fmt.Errorf("%w: symbol dictionary reuses contexts decoded with different parameters", ErrMalformed)
		}
		st.gb = append([]mq.Context(nil), r.gb...)
		st.gr = append([]mq.Context(nil), r.gr...)
	} else {
		st.gb = make([]mq.Context, genericContexts(h.template))
		st.gr = make([]mq.Context, refineContexts(h.refTemplate))
	}
	var newSyms, exported []*Bitmap
	if h.huffman {
		hs, err := newHuffSymbols(h, seg.data[h.dataStart:], tables, st.gr, b)
		if err != nil {
			return nil, err
		}
		if newSyms, err = decodeSymbolsHuffman(ctx, h, inSyms, hs); err != nil {
			return nil, err
		}
		exported, err = exportSymbols(h, inSyms, newSyms, func() (int64, error) {
			return hs.exRun.value(hs.r, "export run length")
		})
		if err != nil {
			return nil, err
		}
	} else {
		st.iaid = newIAID(symbolCodeLen(len(inSyms) + intconv.Clamp(h.numNew)))
		if newSyms, err = decodeSymbols(ctx, h, inSyms, st); err != nil {
			return nil, err
		}
		exported, err = exportSymbols(h, inSyms, newSyms, func() (int64, error) {
			return st.iaex.value(st.dec, "export run length")
		})
		if err != nil {
			return nil, err
		}
	}
	dict := &symbolDict{exported: exported}
	if h.contextRetain {
		dict.retained = &retainedContexts{
			gb: st.gb, gr: st.gr,
			template: h.template, refTemplate: h.refTemplate, refagg: h.refagg,
			at: h.at, refAT: h.refAT,
		}
	}
	return dict, nil
}

// decodeSymbols decodes the height classes of a dictionary, ITU-T T.88, 6.5.5
// step 4, with arithmetic coding.
func decodeSymbols(ctx context.Context, h symbolHeader, inSyms []*Bitmap, st *arithState) ([]*Bitmap, error) {
	// all holds the input symbols followed by those decoded so far, the array
	// an aggregation draws on (6.5.8.2.4), grown in place rather than rebuilt
	// per symbol.
	all := make([]*Bitmap, len(inSyms), int64(len(inSyms))+h.numNew)
	copy(all, inSyms)
	var height int64
	// A height class that ends before its first symbol makes no progress, and
	// past the end of its data the decoder can produce such classes forever.
	// Allowing as many empty classes as symbols bounds the loop at twice the
	// declared count without refusing an encoder that emits a few.
	var empty int64
	decoded := func() int64 { return int64(len(all) - len(inSyms)) }
	for decoded() < h.numNew {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		before := len(all)
		dh, err := st.iadh.value(st.dec, "height class delta height")
		if err != nil {
			return nil, err
		}
		height += dh
		if height < 0 || height > st.budget.byteCap() {
			return nil, fmt.Errorf("%w: height class height %d", ErrMalformed, height)
		}
		var width int64
		for {
			// 6.5.7: OOB ends the height class.
			dw, ok := st.iadw.decode(st.dec)
			if !ok {
				break
			}
			if decoded() >= h.numNew {
				return nil, fmt.Errorf("%w: height classes hold more than the %d symbols declared", ErrMalformed, h.numNew)
			}
			width += dw
			if width < 0 || bitmapExceeds(width, height, st.budget.byteCap()) {
				return nil, fmt.Errorf("%w: symbol of %dx%d pixels", ErrMalformed, width, height)
			}
			bs, err := decodeSymbolBitmap(ctx, h, intconv.Clamp(width), intconv.Clamp(height), all, st)
			if err != nil {
				return nil, fmt.Errorf("symbol %d: %w", decoded(), err)
			}
			all = append(all, bs)
		}
		if len(all) == before {
			if empty++; empty > h.numNew {
				return nil, fmt.Errorf("%w: more empty height classes than symbols", ErrMalformed)
			}
		}
	}
	return all[len(inSyms):], nil
}

// decodeSymbolBitmap decodes one symbol's bitmap: directly with the generic
// region procedure (6.5.8.1, Table 16), or by refinement and aggregation of
// syms, the input symbols and those decoded before it (6.5.8.2).
func decodeSymbolBitmap(ctx context.Context, h symbolHeader, width, height int, syms []*Bitmap, st *arithState) (*Bitmap, error) {
	if !h.refagg {
		if err := st.budget.charge(int64(width), int64(height)); err != nil {
			return nil, err
		}
		return decodeGeneric(ctx, genericParams{
			width: width, height: height, template: h.template, at: h.at,
		}, st.dec, st.gb)
	}

	instances, err := st.iaai.value(st.dec, "aggregation instance count")
	if err != nil {
		return nil, err
	}
	switch {
	case instances > 1:
		// Table 17.
		return decodeText(ctx, textParams{
			width: width, height: height,
			numInstances: instances,
			strips:       1,
			syms:         syms,
			combineOp:    combineOr,
			refCorner:    cornerTopLeft,
			refine:       true,
			refTemplate:  h.refTemplate,
			refAT:        h.refAT,
		}, st)
	case instances == 1:
		// 6.5.8.2.2 and Table 18.
		id := st.iaid.decode(st.dec)
		if id >= int64(len(syms)) {
			return nil, fmt.Errorf("%w: symbol ID %d of %d symbols", ErrMalformed, id, len(syms))
		}
		rdx, err := st.iardx.value(st.dec, "refinement X offset")
		if err != nil {
			return nil, err
		}
		rdy, err := st.iardy.value(st.dec, "refinement Y offset")
		if err != nil {
			return nil, err
		}
		return decodeRefinement(ctx, refineParams{
			width: width, height: height,
			template:  h.refTemplate,
			reference: syms[id],
			dx:        rdx,
			dy:        rdy,
			at:        h.refAT,
		}, st)
	default:
		return nil, fmt.Errorf("%w: aggregation of %d symbol instances", ErrMalformed, instances)
	}
}

// exportSymbols decodes the export flags, ITU-T T.88 6.5.10, as runs of
// alternating value starting with not-exported, read by nextRun (IAEX, or
// Table B.1 in the Huffman variant), and returns the exported symbols in
// input-then-new order.
func exportSymbols(h symbolHeader, inSyms, newSyms []*Bitmap, nextRun func() (int64, error)) ([]*Bitmap, error) {
	total := int64(len(inSyms) + len(newSyms))
	exported := make([]*Bitmap, 0, h.numExported)
	var index int64
	export := false
	// Zero-length runs make no progress; every useful run but the first is at
	// least one symbol long, so more than total+1 runs cannot be well formed.
	for runs := int64(0); index < total; runs++ {
		if runs > total+1 {
			return nil, fmt.Errorf("%w: export flags never cover the %d symbols", ErrMalformed, total)
		}
		run, err := nextRun()
		if err != nil {
			return nil, err
		}
		if run < 0 || index+run > total {
			return nil, fmt.Errorf("%w: export run of %d at symbol %d of %d", ErrMalformed, run, index, total)
		}
		if export {
			for i := index; i < index+run; i++ {
				if i < int64(len(inSyms)) {
					exported = append(exported, inSyms[i])
				} else {
					exported = append(exported, newSyms[i-int64(len(inSyms))])
				}
			}
		}
		index += run
		export = !export
	}
	if int64(len(exported)) != h.numExported {
		return nil, fmt.Errorf("%w: dictionary exports %d symbols, its header declares %d", ErrMalformed, len(exported), h.numExported)
	}
	return exported, nil
}

// huffSymbols is the state of a Huffman-coded symbol dictionary: its reader,
// the tables 7.4.2.1.6 selects, and the fixed tables its aggregations use.
type huffSymbols struct {
	r                              *bitReader
	dh, dw, bmSize, aggInst, exRun *huffTable
	// agg holds the fields of an aggregation's text region, Table 17.
	agg    huffText
	idLen  int
	budget *budget
}

// pickTable returns standard table B.n for a selection value mapped in
// choices, or the next user-supplied table for the value 3 (or 1 for a
// one-bit selection), which T.88 7.4.2.1.6 and 7.4.3.1.6 hand out in the
// order the tables segments are referred to.
func pickTable(sel int, choices map[int]int, userSel int, user []*huffTable, next *int) (*huffTable, error) {
	if sel == userSel {
		if *next >= len(user) {
			return nil, fmt.Errorf("%w: segment selects more user tables than it refers to", ErrMalformed)
		}
		t := user[*next]
		*next++
		return t, nil
	}
	n, ok := choices[sel]
	if !ok {
		return nil, fmt.Errorf("%w: Huffman table selection %d is not permitted", ErrMalformed, sel)
	}
	return standardTable(n)
}

func newHuffSymbols(h symbolHeader, data []byte, user []*huffTable, gr []mq.Context, b *budget) (*huffSymbols, error) {
	hs := &huffSymbols{r: &bitReader{data: data}, budget: b}
	next := 0
	var err error
	if hs.dh, err = pickTable(int(h.flags>>2&3), map[int]int{0: 4, 1: 5}, 3, user, &next); err != nil {
		return nil, err
	}
	if hs.dw, err = pickTable(int(h.flags>>4&3), map[int]int{0: 2, 1: 3}, 3, user, &next); err != nil {
		return nil, err
	}
	if hs.bmSize, err = pickTable(int(h.flags>>6&1), map[int]int{0: 1}, 1, user, &next); err != nil {
		return nil, err
	}
	if hs.aggInst, err = pickTable(int(h.flags>>7&1), map[int]int{0: 1}, 1, user, &next); err != nil {
		return nil, err
	}
	if hs.exRun, err = standardTable(1); err != nil {
		return nil, err
	}
	// Table 17: the text region of an aggregation uses fixed tables.
	hs.agg = huffText{r: hs.r, gr: gr, budget: b}
	for _, pick := range []struct {
		dst **huffTable
		n   int
	}{{&hs.agg.fs, 6}, {&hs.agg.ds, 8}, {&hs.agg.dt, 11}, {&hs.agg.rdw, 15}, {&hs.agg.rdh, 15}, {&hs.agg.rdx, 15}, {&hs.agg.rdy, 15}, {&hs.agg.rsize, 1}} {
		if *pick.dst, err = standardTable(pick.n); err != nil {
			return nil, err
		}
	}
	return hs, nil
}

// decodeSymbolsHuffman decodes the height classes of a Huffman-coded
// dictionary, ITU-T T.88 6.5.5 step 4: symbol by symbol when SDREFAGG is 1,
// otherwise as one collective bitmap per class (6.5.9).
func decodeSymbolsHuffman(ctx context.Context, h symbolHeader, inSyms []*Bitmap, hs *huffSymbols) ([]*Bitmap, error) {
	all := make([]*Bitmap, len(inSyms), int64(len(inSyms))+h.numNew)
	copy(all, inSyms)
	// 6.5.8.2.3: fixed-length IDs over every symbol the dictionary can see.
	hs.idLen = max(symbolCodeLen(len(inSyms)+intconv.Clamp(h.numNew)), 1)
	hs.agg.idLen = hs.idLen
	decoded := func() int64 { return int64(len(all) - len(inSyms)) }
	var height, empty int64
	for decoded() < h.numNew {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		before := len(all)
		dh, err := hs.dh.value(hs.r, "height class delta height")
		if err != nil {
			return nil, err
		}
		height += dh
		if height < 0 || height > hs.budget.byteCap() {
			return nil, fmt.Errorf("%w: height class height %d", ErrMalformed, height)
		}
		var width, total int64
		var widths []int64
		for {
			dw, ok, err := hs.dw.decode(hs.r)
			if err != nil {
				return nil, err
			}
			if !ok {
				break
			}
			if decoded()+int64(len(widths)) >= h.numNew {
				return nil, fmt.Errorf("%w: height classes hold more than the %d symbols declared", ErrMalformed, h.numNew)
			}
			width += dw
			total += width
			if limit := hs.budget.byteCap(); width < 0 || bitmapExceeds(width, height, limit) || bitmapExceeds(total, height, limit) {
				return nil, fmt.Errorf("%w: symbol of %dx%d pixels", ErrMalformed, width, height)
			}
			if !h.refagg {
				widths = append(widths, width)
				continue
			}
			bs, err := decodeSymbolBitmapHuffman(ctx, h, intconv.Clamp(width), intconv.Clamp(height), all, hs)
			if err != nil {
				return nil, fmt.Errorf("symbol %d: %w", decoded(), err)
			}
			all = append(all, bs)
		}
		if !h.refagg {
			syms, err := collectiveSymbols(ctx, widths, total, height, hs)
			if err != nil {
				return nil, err
			}
			all = append(all, syms...)
		}
		if len(all) == before {
			if empty++; empty > h.numNew {
				return nil, fmt.Errorf("%w: more empty height classes than symbols", ErrMalformed)
			}
		}
	}
	return all[len(inSyms):], nil
}

// collectiveSymbols decodes a height class collective bitmap, ITU-T T.88
// 6.5.9, and cuts it into the class's symbols, left to right (6.5.5 step 4 d).
func collectiveSymbols(ctx context.Context, widths []int64, total, height int64, hs *huffSymbols) ([]*Bitmap, error) {
	size, err := hs.bmSize.value(hs.r, "collective bitmap size")
	if err != nil {
		return nil, err
	}
	hs.r.align()
	if err := hs.budget.charge(total, height); err != nil {
		return nil, err
	}
	stride := (total + 7) / 8
	var collective *Bitmap
	if size == 0 {
		// Step 3: stored uncompressed, rows padded to whole bytes, which is
		// exactly the Bitmap layout.
		size = stride * height
		if size > int64(len(hs.r.data)-hs.r.pos) {
			return nil, fmt.Errorf("%w: uncompressed collective bitmap of %d bytes, %d remain", ErrMalformed, size, len(hs.r.data)-hs.r.pos)
		}
		if collective, err = newBitmap(intconv.Clamp(total), intconv.Clamp(height)); err != nil {
			return nil, err
		}
		copy(collective.Data, hs.r.data[hs.r.pos:hs.r.pos+intconv.Clamp(size)])
	} else {
		if size < 0 || size > int64(len(hs.r.data)-hs.r.pos) {
			return nil, fmt.Errorf("%w: collective bitmap of %d bytes, %d remain", ErrMalformed, size, len(hs.r.data)-hs.r.pos)
		}
		// Table 19: MMR with a known byte count, so EOFB may be absent.
		if collective, err = decodeMMR(ctx, intconv.Clamp(total), intconv.Clamp(height), hs.r.data[hs.r.pos:hs.r.pos+intconv.Clamp(size)]); err != nil {
			return nil, err
		}
	}
	hs.r.pos += intconv.Clamp(size)

	syms := make([]*Bitmap, len(widths))
	var x int64
	for i, w := range widths {
		if syms[i], err = collective.columns(x, w); err != nil {
			return nil, err
		}
		x += w
	}
	return syms, nil
}

// decodeSymbolBitmapHuffman decodes a refined or aggregated symbol in a
// Huffman-coded dictionary, ITU-T T.88 6.5.8.2.
func decodeSymbolBitmapHuffman(ctx context.Context, h symbolHeader, width, height int, syms []*Bitmap, hs *huffSymbols) (*Bitmap, error) {
	instances, err := hs.aggInst.value(hs.r, "aggregation instance count")
	if err != nil {
		return nil, err
	}
	switch {
	case instances > 1:
		return decodeText(ctx, textParams{
			width: width, height: height,
			numInstances: instances,
			strips:       1,
			syms:         syms,
			combineOp:    combineOr,
			refCorner:    cornerTopLeft,
			refine:       true,
			refTemplate:  h.refTemplate,
			refAT:        h.refAT,
		}, &hs.agg)
	case instances == 1:
		// 6.5.8.2.2: a fixed-length ID, then RDX and RDY with Table B.15 and
		// the refinement's size with Table B.1.
		id, err := hs.agg.symbolID()
		if err != nil {
			return nil, err
		}
		if id >= int64(len(syms)) {
			return nil, fmt.Errorf("%w: symbol ID %d of %d symbols", ErrMalformed, id, len(syms))
		}
		rdx, err := hs.agg.rdx.value(hs.r, "refinement X offset")
		if err != nil {
			return nil, err
		}
		rdy, err := hs.agg.rdy.value(hs.r, "refinement Y offset")
		if err != nil {
			return nil, err
		}
		block, err := hs.agg.refinementBlock()
		if err != nil {
			return nil, err
		}
		st := &arithState{dec: mq.NewDecoder(block), gr: hs.agg.gr, budget: hs.budget}
		return decodeRefinement(ctx, refineParams{
			width: width, height: height,
			template:  h.refTemplate,
			reference: syms[id],
			dx:        rdx,
			dy:        rdy,
			at:        h.refAT,
		}, st)
	default:
		return nil, fmt.Errorf("%w: aggregation of %d symbol instances", ErrMalformed, instances)
	}
}
