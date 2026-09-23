package jbig2

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/filter/mq"
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
// whose input symbols are inSyms and whose last referred-to dictionary, if
// any, is last.
func decodeSymbolDict(ctx context.Context, seg segment, inSyms []*Bitmap, last *symbolDict, b *budget) (*symbolDict, error) {
	h, err := parseSymbolHeader(seg.data)
	if err != nil {
		return nil, err
	}
	if h.huffman {
		return nil, fmt.Errorf("%w: Huffman-coded symbol dictionary", ErrUnsupported)
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
	st.iaid = newIAID(symbolCodeLen(len(inSyms) + int(h.numNew)))

	newSyms, err := decodeSymbols(ctx, h, inSyms, st)
	if err != nil {
		return nil, err
	}
	exported, err := exportSymbols(h, inSyms, newSyms, st)
	if err != nil {
		return nil, err
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
		if height < 0 || height > maxBitmapPixels {
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
			if width < 0 || areaExceeds(width, height, maxBitmapPixels) {
				return nil, fmt.Errorf("%w: symbol of %dx%d pixels", ErrMalformed, width, height)
			}
			bs, err := decodeSymbolBitmap(ctx, h, int(width), int(height), all, st)
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
// alternating value starting with not-exported, and returns the exported
// symbols in input-then-new order.
func exportSymbols(h symbolHeader, inSyms, newSyms []*Bitmap, st *arithState) ([]*Bitmap, error) {
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
		run, err := st.iaex.value(st.dec, "export run length")
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
