package varfont

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// cffSfntVersion is the sfnt version of an OpenType program with CFF
// outlines, 'OTTO' (OpenType specification, Organization of an OpenType
// Font, Table Directory).
const cffSfntVersion = 0x4F54544F

// IsCFF2 reports whether program, an sfnt program, has CFF2 outlines and
// no 'glyf' table: a program a PDF cannot embed as it stands, since a font
// file holds TrueType or CFF outlines (ISO 32000-1, 9.9, Table 126) and
// only an OpenType font file of PDF 2.0 may hold CFF2 ones. A program
// whose table directory does not read is not one.
func IsCFF2(program []byte) bool {
	records, err := truetype.ParseTableDirectory(program)
	if err != nil {
		return false
	}
	return (&Font{records: records}).HasCFF2()
}

// DefaultCFF returns program, an sfnt program with CFF2 outlines (see
// [IsCFF2]), at its default instance with CFF outlines: the program
// [Font.Instance] writes at the default coordinates for a variable font,
// and for a font without 'fvar' the same conversion with every blend at
// its default value and the program's own names kept. It fails as
// [Font.Instance] does, with [ErrUnsupported] for a program without CFF2
// outlines, and with an error wrapping [truetype.ErrTableNotFound] for a
// program missing a table the conversion needs.
func DefaultCFF(ctx context.Context, program []byte) ([]byte, error) {
	if !IsCFF2(program) {
		return nil, fmt.Errorf("%w: no CFF2 outlines", ErrUnsupported)
	}
	f, err := Parse(program)
	switch {
	case errors.Is(err, truetype.ErrTableNotFound):
		records, rerr := truetype.ParseTableDirectory(program)
		if rerr != nil {
			return nil, rerr
		}
		f = &Font{program: program, records: records, axes: &Axes{}}
	case err != nil:
		return nil, err
	}
	return f.Instance(ctx, nil)
}

// instanceCFF2 writes the instance of a program with CFF2 outlines: the
// 'CFF2' table becomes a 'CFF ' table at the instance (see
// [cff.InstanceCFF2]), the advances take their 'HVAR' deltas and the side
// bearings the instance outlines' boxes, and the font-wide values their
// 'MVAR' deltas. A program without 'HVAR' keeps its advances.
//
// The CFF2 outlines are written as CFF rather than as CFF2 without
// variations: a PDF embeds a bare CFF program as CIDFontType0C and an
// OpenType one as OpenType (ISO 32000-1, 9.9, Table 126; ISO 32000-2,
// 9.9.2, Table 124), and PDF 2.0 alone names OpenType, whose CFF2 form
// readers of earlier PDFs do not draw.
//
// A font without axes, a CFF2 font without 'fvar', is written at its
// default instance, every region of its variation stores at 0, under its
// own PostScript name.
func (in *instancer) instanceCFF2(ctx context.Context, tables map[string][]byte, user []float64) ([]byte, error) {
	static := len(in.f.axes.Axes) == 0
	if static {
		in.coords = nil
	}
	head, hhea, maxp, hmtx := tables["head"], tables["hhea"], tables["maxp"], tables["hmtx"]
	if len(head) < headSize || len(hhea) < hheaSize || len(maxp) < maxpMinSize || hmtx == nil {
		return nil, fmt.Errorf("%w: head, hhea, maxp or hmtx missing or short", ErrInvalid)
	}
	in.n = int(binary.BigEndian.Uint16(maxp[maxpNumGlyphsAt:]))
	var err error
	if in.h, err = readMetrics(hmtx, int(binary.BigEndian.Uint16(hhea[hheaNumberOfMetricsAt:])), in.n); err != nil {
		return nil, fmt.Errorf("hmtx: %w", err)
	}
	if data := tables["HVAR"]; data != nil {
		if in.hvar, err = parseAdvanceVariations(data, in.coords, false); err != nil {
			return nil, fmt.Errorf("HVAR: %w", err)
		}
	}
	advances := make([]uint16, in.n)
	widths := make([]float64, in.n)
	for gid := range in.n {
		adv := int32(in.h[gid].advance)
		if in.hvar != nil {
			adv += otRound(in.hvar.delta(gid))
		}
		advances[gid] = uint16(min(max(adv, 0), math.MaxUint16)) //nolint:gosec // clamped to the uint16 range first
		widths[gid] = float64(advances[gid])
	}
	name := PostScriptName(in.f.axes, tables["name"], user)
	if own := psChars(nameString(tables["name"], namePostScript)); static && own != "" {
		name = own
	}
	coords := in.coords
	inst, err := cff.InstanceCFF2(ctx, tables["CFF2"], name, widths, func(store []byte) ([][]float64, error) {
		s, err := parseItemStore(store, coords)
		if err != nil {
			return nil, err
		}
		out := make([][]float64, len(s.data))
		for i, d := range s.data {
			out[i] = make([]float64, len(d.regions))
			for j, r := range d.regions {
				out[i][j] = s.scalars[r]
			}
		}
		return out, nil
	})
	switch {
	case errors.Is(err, cff.ErrInvalidCFF), errors.Is(err, cff.ErrInvalidCharstring), errors.Is(err, cff.ErrTruncated):
		return nil, fmt.Errorf("%w: CFF2: %w", ErrInvalid, err)
	case err != nil:
		return nil, err
	}
	if len(inst.Boxes) != in.n {
		return nil, fmt.Errorf("%w: CFF2 holds %d glyphs, maxp %d", ErrInvalid, len(inst.Boxes), in.n)
	}

	out := map[string][]byte{"CFF ": inst.Program}
	hm := make([]byte, 0, 4*in.n)
	hh := slices.Clone(hhea)
	var advMax uint16
	minLSB, minRSB, maxExtent := int16(math.MaxInt16), int16(math.MaxInt16), int16(math.MinInt16)
	box := [4]int16{math.MaxInt16, math.MaxInt16, math.MinInt16, math.MinInt16}
	for gid := range in.n {
		b := inst.Boxes[gid]
		gb := [4]int16{clampInt16(int32(math.Floor(b[0]))), clampInt16(int32(math.Floor(b[1]))), clampInt16(int32(math.Ceil(b[2]))), clampInt16(int32(math.Ceil(b[3])))}
		lsb := gb[0]
		if inst.Empty[gid] {
			lsb = 0
		}
		hm = binary.BigEndian.AppendUint16(hm, advances[gid])
		hm = binary.BigEndian.AppendUint16(hm, uint16(lsb)) //nolint:gosec // writes the 16 bits of a signed FWORD
		advMax = max(advMax, advances[gid])
		if inst.Empty[gid] {
			continue
		}
		minLSB = min(minLSB, lsb)
		minRSB = min(minRSB, clampInt16(int32(advances[gid])-int32(gb[2])))
		maxExtent = max(maxExtent, gb[2])
		box = [4]int16{min(box[0], gb[0]), min(box[1], gb[1]), max(box[2], gb[2]), max(box[3], gb[3])}
	}
	if box[0] > box[2] {
		box = [4]int16{}
		minLSB, minRSB, maxExtent = 0, 0, 0
	}
	putU16(hh, hheaAdvanceMaxAt, advMax)
	putI16(hh, hheaMinLSBAt, minLSB)
	putI16(hh, hheaMinRSBAt, minRSB)
	putI16(hh, hheaXMaxExtentAt, maxExtent)
	putU16(hh, hheaNumberOfMetricsAt, uint16(in.n)) //nolint:gosec // n came from maxp's uint16 numGlyphs
	hd := slices.Clone(head)
	for i, v := range box {
		putI16(hd, headBoxAt+2*i, v)
	}
	out["hmtx"], out["hhea"], out["head"] = hm, hh, hd
	for _, tag := range []string{"OS/2", "post"} {
		if t := tables[tag]; t != nil {
			out[tag] = slices.Clone(t)
		}
	}
	if err := in.applyMVAR(tables, out); err != nil {
		return nil, err
	}
	in.applyStyle(out, user)
	if nameTable := tables["name"]; nameTable != nil && !static {
		renamed, err := renamePostScript(nameTable, name)
		if err != nil {
			return nil, err
		}
		out["name"] = renamed
	}
	tags := make([]string, 0, len(tables))
	for tag, data := range tables {
		if tag == "CFF2" || slices.Contains(droppedTables, tag) {
			continue
		}
		if _, ok := out[tag]; !ok {
			out[tag] = data
		}
		tags = append(tags, tag)
	}
	tags = append(tags, "CFF ")
	slices.Sort(tags)
	return truetype.AssembleTables(cffSfntVersion, tags, out), nil
}
