package text

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"

	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// maxFootprintFileBytes bounds the bytes [NewFonts] reads from one font file:
// the table directories and the name, OS/2, head, hhea, maxp, cmap and loca
// tables of its faces. Anti-DoS guard, not spec-mandated: those tables weigh
// a few hundred KiB for the largest fonts in use, the pan-CJK collections of
// ten faces included, and the bound keeps a file that lists many faces from
// costing more.
const maxFootprintFileBytes = 32 << 20

// maxSmallTableBytes bounds the head, bhed, hhea, maxp and OS/2 tables the
// index reads. Anti-DoS guard, not spec-mandated: their largest versions
// hold about a hundred bytes (ISO/IEC 14496-22, head, hhea, maxp and OS/2
// tables), so a longer one carries nothing the index reads.
const maxSmallTableBytes = 4 << 10

// maxNameTableBytes bounds the name table the index reads. Anti-DoS guard,
// not spec-mandated: a name table addresses its strings with 16-bit offsets
// and lengths, so everything it can reach fits in about 1 MiB.
const maxNameTableBytes = 1 << 20

// maxCmapTableBytes bounds the cmap table the index reads. Anti-DoS guard,
// not spec-mandated: a format 12 subtable covering every assigned Unicode
// character in its own group weighs about 3 MiB, and real fonts merge their
// groups into far less.
const maxCmapTableBytes = 8 << 20

// maxLocaTableBytes bounds the loca table the index reads to check it. It is
// the largest loca a font may need: 65536 glyphs plus one, four bytes each
// (ISO/IEC 14496-22, loca table). Anti-DoS guard.
const maxLocaTableBytes = 4 * (1<<16 + 1)

// maxSfntTables bounds the table directory of one face. Anti-DoS guard, not
// spec-mandated: the directory counts its tables in a uint16, and the
// OpenType specification registers fewer than a hundred table tags, so a
// face listing more is no font program. It matches the truetype package's
// bound.
const maxSfntTables = 512

// maxCoverageRanges bounds the character ranges the index keeps for one face.
// Anti-DoS guard, not spec-mandated: merged ranges of consecutive characters
// number a few hundred for the largest pan-CJK faces, and a face cut past the
// bound only loses fallback candidacy for its last characters.
const maxCoverageRanges = 1 << 14

// maxProbedCodePoint is the last character the index probes, one by one, in
// a cmap that does not list its coverage as ranges: the legacy formats and
// the symbol and Macintosh encodings go-text remaps all lie in the Basic
// Multilingual Plane.
const maxProbedCodePoint = 0xFFFF

// errReadBound reports a font file whose footprint needs more bytes than
// [maxFootprintFileBytes] or the index budget leaves.
var errReadBound = errors.New("font file footprint past the read bound")

// sfntFile is an open font file: go-text's loader reads it with Read, Seek
// and ReadAt.
type sfntFile interface {
	io.ReaderAt
	io.ReadSeeker
	io.Closer
}

// openFunc opens the font file at path and returns its size.
type openFunc func(path string) (sfntFile, int64, error)

// openOSFile opens a font file of the file system.
func openOSFile(path string) (sfntFile, int64, error) {
	file, err := os.Open(path) //nolint:gosec // reading the font files the caller's directories hold is the purpose of the index
	if err != nil {
		return nil, 0, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close() //nolint:errcheck,gosec // read-only file: Close reports nothing a read did not
		return nil, 0, err
	}
	return file, info.Size(), nil
}

// boundedReader charges every byte read through it to left and fails a read
// that would overdraw it, before the read happens.
type boundedReader struct {
	r    sfntFile
	left int64
}

// Read reads from the file, within the bound.
func (b *boundedReader) Read(p []byte) (int, error) {
	if int64(len(p)) > b.left {
		return 0, errReadBound
	}
	n, err := b.r.Read(p)
	b.left -= int64(n)
	return n, err
}

// ReadAt reads from the file at off, within the bound.
func (b *boundedReader) ReadAt(p []byte, off int64) (int, error) {
	if int64(len(p)) > b.left {
		return 0, errReadBound
	}
	n, err := b.r.ReadAt(p, off)
	b.left -= int64(n)
	return n, err
}

// Seek moves the file offset; it reads nothing.
func (b *boundedReader) Seek(off int64, whence int) (int64, error) {
	return b.r.Seek(off, whence)
}

// fontSource is one indexed font file, shared by its faces.
type fontSource struct {
	path string
	open openFunc
	// collection reports a TrueType or OpenType Collection.
	collection bool
}

// runeRange is a closed range of characters a face maps to a glyph.
type runeRange struct {
	lo, hi rune
}

// readFootprints reads the footprint of every face the font file r of size
// bytes holds, within the bound r charges. A face that fails to read, or that litho cannot use, is
// skipped with a Debug log; the error reports a file none of whose faces can
// be read.
func readFootprints(logger *slog.Logger, src *fontSource, r *boundedReader, size int64) ([]*Face, error) {
	var magic [4]byte
	if _, err := r.ReadAt(magic[:], 0); err != nil {
		return nil, fmt.Errorf("reading the font header: %w", err)
	}
	src.collection = truetype.IsCollection(magic[:])
	if !src.collection && size > maxFontFileBytes {
		return nil, fmt.Errorf("program of %d bytes past the %d-byte bound", size, maxFontFileBytes)
	}
	dirs, err := truetype.Faces(r, size)
	if err != nil {
		return nil, err
	}
	records := make([][]truetype.TableRecord, len(dirs))
	for i, dir := range dirs {
		if records[i], err = readTableDirectory(r, size, dir); err != nil {
			return nil, fmt.Errorf("face %d: %w", i, err)
		}
	}
	// go-text's loader parses the directories litho has just checked.
	loaders, err := ot.NewLoaders(r)
	if err != nil {
		return nil, err
	}
	if len(loaders) != len(dirs) {
		return nil, fmt.Errorf("go-text reads %d faces, the header lists %d", len(loaders), len(dirs))
	}
	faces := make([]*Face, 0, len(dirs))
	for i, ld := range loaders {
		fc, err := readFootprint(ld, records[i])
		if err != nil {
			logger.Debug("font files: skipping a face", slog.String("path", src.path), slog.Int("face", i), slog.Any("error", err))
			continue
		}
		fc.src, fc.index, fc.dir = src, i, dirs[i]
		faces = append(faces, fc)
	}
	return faces, nil
}

// readTableDirectory reads the table directory starting at dir in r, of
// length size, and checks that every table it lists lies within size.
func readTableDirectory(r io.ReaderAt, size, dir int64) ([]truetype.TableRecord, error) {
	var head [12]byte
	if dir < 0 || dir+int64(len(head)) > size {
		return nil, fmt.Errorf("%w: table directory past the end of the file", truetype.ErrInvalidFont)
	}
	if _, err := r.ReadAt(head[:], dir); err != nil {
		return nil, fmt.Errorf("reading the table directory: %w", err)
	}
	if !truetype.IsSfnt(head[:]) {
		return nil, fmt.Errorf("%w: not an sfnt program", truetype.ErrInvalidFont)
	}
	n := int(binary.BigEndian.Uint16(head[4:6]))
	if n > maxSfntTables {
		return nil, fmt.Errorf("%w: %d tables", truetype.ErrInvalidFont, n)
	}
	raw := make([]byte, len(head)+16*n)
	if dir+int64(len(raw)) > size {
		return nil, fmt.Errorf("%w: table directory past the end of the file", truetype.ErrInvalidFont)
	}
	if _, err := r.ReadAt(raw, dir); err != nil {
		return nil, fmt.Errorf("reading the table directory: %w", err)
	}
	records, err := truetype.ParseTableDirectory(raw)
	if err != nil {
		return nil, err
	}
	for _, rec := range records {
		if int64(rec.Offset)+int64(rec.Length) > size {
			return nil, fmt.Errorf("%w: table %q past the end of the file", truetype.ErrInvalidFont, rec.Tag)
		}
	}
	return records, nil
}

// findRecord returns the first record of tag, as go-text's loader keeps the
// first of duplicate tables.
func findRecord(records []truetype.TableRecord, tag string) (truetype.TableRecord, bool) {
	for _, rec := range records {
		if rec.Tag == tag {
			return rec, true
		}
	}
	return truetype.TableRecord{}, false
}

// readTable returns the table tag of ld, nil when the face has none, and
// refuses one longer than limit before reading it.
func readTable(ld *ot.Loader, records []truetype.TableRecord, tag string, limit uint32) ([]byte, error) {
	rec, ok := findRecord(records, tag)
	if !ok {
		return nil, nil
	}
	if rec.Length > limit {
		return nil, fmt.Errorf("table %q of %d bytes past the %d-byte bound", tag, rec.Length, limit)
	}
	data, err := ld.RawTable(ot.MustNewTag(tag))
	if err != nil {
		return nil, fmt.Errorf("reading table %q: %w", tag, err)
	}
	return data, nil
}

// readHead returns the face's font header: bhed, Apple's bitmap header,
// when the face has one, as go-text reads it first, else head.
func readHead(ld *ot.Loader, records []truetype.TableRecord) ([]byte, error) {
	head, err := readTable(ld, records, "bhed", maxSmallTableBytes)
	if err == nil && head == nil {
		head, err = readTable(ld, records, "head", maxSmallTableBytes)
	}
	return head, err
}

// checkProgram refuses a face go-text could not load or litho could not
// embed: one without outlines, a colour bitmap face, a face listing a table
// twice, and a face whose tables sum past [maxFontFileBytes], which a
// collection face extracted on [Face.Compose] would exceed. It returns the
// sum of the face's table lengths.
func checkProgram(records []truetype.TableRecord) (int64, error) {
	var total int64
	outlines, bitmap := false, false
	seen := make(map[string]bool, len(records))
	for _, rec := range records {
		if seen[rec.Tag] {
			return 0, fmt.Errorf("%w: table %q listed twice", truetype.ErrInvalidFont, rec.Tag)
		}
		seen[rec.Tag] = true
		switch rec.Tag {
		case "glyf", "CFF ", "CFF2":
			outlines = true
		case "CBDT", "sbix":
			bitmap = true
		}
		total += int64(rec.Length)
	}
	switch {
	case bitmap:
		// Noto Color Emoji and Apple Color Emoji draw with colour bitmaps,
		// which the backend cannot embed as outlines.
		return 0, errors.New("colour bitmap face, not embeddable as outlines")
	case !outlines:
		return 0, errors.New("face without glyf, CFF or CFF2 outlines")
	case total > maxFontFileBytes:
		return 0, fmt.Errorf("face of %d bytes past the %d-byte bound", total, maxFontFileBytes)
	}
	return total, nil
}

// readFootprint reads what matching and layout need of the face ld loads,
// whose table directory is records.
func readFootprint(ld *ot.Loader, records []truetype.TableRecord) (*Face, error) {
	total, err := checkProgram(records)
	if err != nil {
		return nil, err
	}
	head, err := readHead(ld, records)
	if err != nil {
		return nil, err
	}
	headTable, _, err := tables.ParseHead(head)
	if err != nil {
		return nil, fmt.Errorf("parsing head: %w", err)
	}
	maxp, err := readTable(ld, records, "maxp", maxSmallTableBytes)
	if err != nil {
		return nil, err
	}
	if _, _, err := tables.ParseMaxp(maxp); err != nil {
		return nil, fmt.Errorf("parsing maxp: %w", err)
	}
	cmap, err := readTable(ld, records, "cmap", maxCmapTableBytes)
	if err != nil {
		return nil, err
	}
	loca, err := readTable(ld, records, "loca", maxLocaTableBytes)
	if err != nil {
		return nil, err
	}
	glyf, _ := findRecord(records, "glyf")
	rawHhea, err := readTable(ld, records, "hhea", maxSmallTableBytes)
	if err != nil {
		return nil, err
	}
	vhea, err := readTable(ld, records, "vhea", maxSmallTableBytes)
	if err != nil {
		return nil, err
	}
	if err := checkGoTextSafe(goTextTables{cmap: cmap, head: head, maxp: maxp, loca: loca, hhea: rawHhea, vhea: vhea, glyfLen: int64(glyf.Length)}); err != nil {
		return nil, err
	}
	rawOS2, err := readTable(ld, records, "OS/2", maxSmallTableBytes)
	if err != nil {
		return nil, err
	}
	os2, _, os2Err := tables.ParseOs2(rawOS2)
	hasOS2 := rawOS2 != nil && os2Err == nil
	cmapTable, _, err := tables.ParseCmap(cmap)
	if err != nil {
		return nil, fmt.Errorf("parsing cmap: %w", err)
	}
	coverage, err := cmapCoverage(cmapTable, os2.FontPage())
	if err != nil {
		return nil, err
	}
	rawName, err := readTable(ld, records, "name", maxNameTableBytes)
	if err != nil {
		return nil, err
	}
	names, _, _ := tables.ParseName(rawName) //nolint:errcheck // a face without a readable name table matches no family by name and stays a fallback candidate
	fc := &Face{
		upem:     float64(headTable.Upem()),
		coverage: coverage,
		size:     total,
	}
	fc.setNames(names)
	fc.setAspect(hasOS2, os2, headTable.MacStyle, names.Name(nameSubfamily))
	hhea, _, hheaErr := tables.ParseHhea(rawHhea)
	_, hasHmtx := findRecord(records, "hmtx")
	fc.setMetrics(hasOS2, os2, rawHhea != nil && hheaErr == nil && hasHmtx, hhea)
	return fc, nil
}

// Name IDs the index reads (ISO/IEC 14496-22, name table, Name IDs).
const (
	nameFamily            tables.NameID = 1
	nameSubfamily         tables.NameID = 2
	nameFull              tables.NameID = 4
	namePostScript        tables.NameID = 6
	nameTypographicFamily tables.NameID = 16
)

// setNames records the face's family names: the typographic family (name ID
// 16) and the family (name ID 1), both matched by [Fonts.Match].
func (fc *Face) setNames(names tables.Name) {
	typographic := strings.TrimSpace(names.Name(nameTypographicFamily))
	family := strings.TrimSpace(names.Name(nameFamily))
	fc.family = typographic
	if fc.family == "" {
		fc.family = family
	}
	for _, name := range []string{typographic, family} {
		if k := familyKey(name); k != "" && !slices.Contains(fc.keys, k) {
			fc.keys = append(fc.keys, k)
		}
	}
	fc.postScript = names.Name(namePostScript)
	fc.fullName = names.Name(nameFull)
}

// OS/2 fsSelection bits (ISO/IEC 14496-22, OS/2 table, fsSelection).
const (
	fsSelectionItalic         = 1 << 0
	fsSelectionUseTypoMetrics = 1 << 7
	fsSelectionOblique        = 1 << 9
)

// macStyleItalic is the italic bit of the head table's macStyle.
const macStyleItalic = 1 << 1

// setAspect records the face's weight, slant and width: from the OS/2 table
// when the face has one, else from head's macStyle and the subfamily name,
// as fontconfig does (fcfreetype.c).
func (fc *Face) setAspect(hasOS2 bool, os2 tables.Os2, macStyle uint16, subfamily string) {
	sub := strings.ToLower(subfamily)
	fc.weight = 0
	fc.width = widthNormal
	if hasOS2 {
		w := int(os2.USWeightClass)
		if w > 0 && w < 10 {
			// Some fonts state the weight on the old 1 to 9 scale.
			w *= 100
		}
		fc.weight = min(w, 1000)
		if c := int(os2.USWidthClass); c >= 1 && c <= 9 {
			fc.width = c
		}
		switch {
		case os2.FsSelection&fsSelectionItalic != 0:
			fc.slant = slantItalic
		case os2.FsSelection&fsSelectionOblique != 0:
			fc.slant = slantOblique
		}
	}
	if fc.weight == 0 {
		fc.weight = weightFromName(sub)
	}
	if fc.slant == slantNormal {
		switch {
		case macStyle&macStyleItalic != 0 || strings.Contains(sub, "italic"):
			fc.slant = slantItalic
		case strings.Contains(sub, "oblique"):
			fc.slant = slantOblique
		}
	}
}

// weightFromName returns the CSS weight a lower-case subfamily name states,
// 400 when it states none, after fontconfig's weight names (fcfreetype.c).
func weightFromName(sub string) int {
	for _, w := range []struct {
		name   string
		weight int
	}{
		{"extrablack", 950},
		{"ultrablack", 950},
		{"extrabold", 800},
		{"ultrabold", 800},
		{"semibold", 600},
		{"demibold", 600},
		{"extralight", 200},
		{"ultralight", 200},
		{"black", 900},
		{"heavy", 900},
		{"bold", 700},
		{"medium", 500},
		{"light", 300},
		{"thin", 100},
	} {
		if strings.Contains(sub, w.name) {
			return w.weight
		}
	}
	return 400
}

// setMetrics records the face's vertical metrics as go-text's font extents
// read them: the OS/2 typographic metrics when the face sets
// USE_TYPO_METRICS and its OS/2 table carries data, else hhea's when the
// face has hhea and hmtx, else 0.8 and 0.2 em.
func (fc *Face) setMetrics(hasOS2 bool, os2 tables.Os2, hasHhea bool, hhea tables.Hhea) {
	useTypo := hasOS2 && os2.FsSelection&fsSelectionUseTypoMetrics != 0 &&
		(os2.USWeightClass != 0 || os2.USWidthClass != 0 || os2.USFirstCharIndex != 0 || os2.USLastCharIndex != 0) &&
		(os2.Version < 2 || len(os2.HigherVersionData) >= 12)
	abs := func(v int16) float64 { return max(float64(v), -float64(v)) }
	switch {
	case useTypo:
		fc.ascent, fc.descent, fc.lineGap = abs(os2.STypoAscender), abs(os2.STypoDescender), float64(os2.STypoLineGap)
	case hasHhea:
		fc.ascent, fc.descent, fc.lineGap = abs(hhea.Ascender), abs(hhea.Descender), float64(hhea.LineGap)
	default:
		fc.ascent, fc.descent = 0.8*fc.upem, 0.2*fc.upem
	}
	// OS/2 version 2 adds sxHeight after the two code page ranges.
	if hasOS2 && os2.Version >= 2 && len(os2.HigherVersionData) >= 10 {
		fc.xHeight = max(float64(int16(binary.BigEndian.Uint16(os2.HigherVersionData[8:10]))), 0) //nolint:gosec // sxHeight is a signed FWORD
	}
}

// cmapCoverage returns the characters the cmap go-text selects maps, merged
// into sorted ranges. A cmap that lists ranges gives them; another is probed
// character by character up to [maxProbedCodePoint], which also honours
// go-text's symbol and Macintosh remappings.
func cmapCoverage(cmap tables.Cmap, page tables.FontPage) ([]runeRange, error) {
	cm, _, err := font.ProcessCmap(cmap, page)
	if err != nil {
		return nil, fmt.Errorf("processing cmap: %w", err)
	}
	var ranges []runeRange
	if ranger, ok := cm.(font.CmapRuneRanger); ok {
		for _, rg := range ranger.RuneRanges(nil) {
			if rg[0] <= rg[1] && rg[0] >= 0 {
				ranges = append(ranges, runeRange{rg[0], rg[1]})
			}
		}
	} else {
		for r := rune(0); r <= maxProbedCodePoint; r++ {
			gid, ok := cm.Lookup(r)
			if !ok || gid == 0 {
				continue
			}
			if n := len(ranges); n > 0 && ranges[n-1].hi == r-1 {
				ranges[n-1].hi = r
				continue
			}
			ranges = append(ranges, runeRange{r, r})
		}
	}
	return mergeRanges(ranges), nil
}

// mergeRanges sorts ranges and merges those that overlap or touch, keeping
// at most [maxCoverageRanges]. The result is a fresh slice sized to fit.
func mergeRanges(ranges []runeRange) []runeRange {
	slices.SortFunc(ranges, func(a, b runeRange) int {
		if a.lo != b.lo {
			return int(a.lo) - int(b.lo)
		}
		return int(a.hi) - int(b.hi)
	})
	out := ranges[:0]
	for _, rg := range ranges {
		if n := len(out); n > 0 && rg.lo <= out[n-1].hi+1 {
			out[n-1].hi = max(out[n-1].hi, rg.hi)
			continue
		}
		if len(out) == maxCoverageRanges {
			break
		}
		out = append(out, rg)
	}
	return slices.Clone(out)
}

// covers reports whether the face's cmap maps r, by its footprint.
func (fc *Face) covers(r rune) bool {
	_, found := slices.BinarySearchFunc(fc.coverage, r, func(rg runeRange, r rune) int {
		switch {
		case rg.hi < r:
			return -1
		case rg.lo > r:
			return 1
		}
		return 0
	})
	return found
}
