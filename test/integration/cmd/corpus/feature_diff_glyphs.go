package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
)

// runeRange is a closed range of characters a face covers.
type runeRange struct{ lo, hi rune }

// coverage is the characters a face's cmap maps, as sorted, disjoint
// ranges.
type coverage []runeRange

// covers reports whether c holds r.
func (c coverage) covers(r rune) bool {
	i, found := slices.BinarySearchFunc(c, r, func(rr runeRange, r rune) int {
		switch {
		case rr.hi < r:
			return -1
		case rr.lo > r:
			return 1
		}
		return 0
	})
	return found && i < len(c)
}

// coverageOf folds the characters of a cmap into ranges.
func coverageOf(cmap font.Cmap) coverage {
	var runes []rune
	for it := cmap.Iter(); it.Next(); {
		r, _ := it.Char()
		runes = append(runes, r)
	}
	slices.Sort(runes)
	var c coverage
	for _, r := range runes {
		if n := len(c); n > 0 && r <= c[n-1].hi+1 {
			c[n-1].hi = max(c[n-1].hi, r)
			continue
		}
		c = append(c, runeRange{r, r})
	}
	return c
}

// fixtureFaces indexes the faces of the fixture fonts by their PostScript
// names, folded as [foldFace] folds a reference's: family, then style, to
// the characters the face covers. The first face of a family and style, in
// directory order, is kept.
type fixtureFaces struct {
	byFamily map[string]map[string]coverage
}

// has reports whether the fixture fonts hold a face of family.
func (f *fixtureFaces) has(family string) bool {
	return len(f.byFamily[family]) > 0
}

// face returns the coverage of family's face of style, else of its regular
// face, else of its first style in name order.
func (f *fixtureFaces) face(family, style string) coverage {
	styles := f.byFamily[family]
	if c, ok := styles[style]; ok {
		return c
	}
	if c, ok := styles[""]; ok {
		return c
	}
	if keys := slices.Sorted(maps.Keys(styles)); len(keys) > 0 {
		return styles[keys[0]]
	}
	return nil
}

// maxFixtureFaceFiles bounds the font files one index reads. Anti-DoS guard
// of a walk the testdata could otherwise send through a large tree, not a
// property of any font: the Gotenberg image holds a few hundred files.
const maxFixtureFaceFiles = 10000

// fixtureFaceCache keeps one index per list of font directories, read once
// per process.
var fixtureFaceCache struct {
	sync.Mutex
	byDirs map[string]*fixtureFaces
}

// indexFixtureFaces returns the index of the TrueType and OpenType faces
// under dirs, read once per list.
func indexFixtureFaces(ctx context.Context, dirs []string) (*fixtureFaces, error) {
	key := strings.Join(dirs, "\x00")
	fixtureFaceCache.Lock()
	defer fixtureFaceCache.Unlock()
	if f := fixtureFaceCache.byDirs[key]; f != nil {
		return f, nil
	}
	f := &fixtureFaces{byFamily: make(map[string]map[string]coverage)}
	files := 0
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !isImageFontFile(d.Name()) {
				return err
			}
			if files++; files > maxFixtureFaceFiles {
				return fmt.Errorf("more than %d font files under %s", maxFixtureFaceFiles, dir)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return f.add(p)
		})
		if err != nil {
			return nil, err
		}
	}
	if fixtureFaceCache.byDirs == nil {
		fixtureFaceCache.byDirs = make(map[string]*fixtureFaces)
	}
	fixtureFaceCache.byDirs[key] = f
	return f, nil
}

// postScriptNameID is the name ID of a face's PostScript name (ISO/IEC
// 14496-22, name table, name ID 6).
const postScriptNameID tables.NameID = 6

// add indexes the faces of the font file at p. A file the parser refuses is
// skipped, as litho's own index skips it.
func (f *fixtureFaces) add(p string) error {
	file, err := os.Open(p) //nolint:gosec // a font file under the fixture fonts directory
	if err != nil {
		return err
	}
	defer file.Close()
	loaders, err := ot.NewLoaders(file)
	if err != nil {
		return nil //nolint:nilerr // a file that holds no font program the parser reads is skipped
	}
	for _, ld := range loaders {
		raw, err := ld.RawTable(ot.MustNewTag("name"))
		if err != nil {
			continue
		}
		names, _, err := tables.ParseName(raw)
		if err != nil {
			continue
		}
		family, style := foldFace(names.Name(postScriptNameID))
		if family == "" || f.byFamily[family] != nil && f.byFamily[family][style] != nil {
			continue
		}
		ft, err := font.NewFont(ld)
		if err != nil {
			continue
		}
		if f.byFamily[family] == nil {
			f.byFamily[family] = make(map[string]coverage)
		}
		f.byFamily[family][style] = coverageOf(ft.Cmap)
	}
	return nil
}

// maxToUnicodeRange bounds the characters one bfrange of a ToUnicode CMap
// adds. Anti-DoS guard, not spec-mandated: a two-byte code space holds 65536
// codes, and a subset font maps the few hundred it draws.
const maxToUnicodeRange = 1 << 16

// referenceFaceText returns, for each face a reference PDF draws text with
// by its /BaseFont without the subset tag (ISO 32000-1, 9.6.4), the
// characters its ToUnicode CMap maps codes to (ISO 32000-1, 9.10.3), read
// with qpdf at qpdf. A subset font, as Office embeds every face, maps the
// codes of the glyphs it holds, which are those the reference draws. A face
// without a ToUnicode CMap is left out. qpdf exits 3 when it read the file
// with warnings, and still writes the JSON.
func referenceFaceText(ctx context.Context, qpdf, path string) (map[string][]rune, error) {
	objects, err := qpdfObjects(ctx, qpdf, path, "none", nil)
	if err != nil {
		return nil, err
	}
	faces := make(map[string][]string)
	for _, raw := range objects {
		var obj struct {
			Value  map[string]json.RawMessage `json:"value"`
			Stream struct {
				Dict map[string]json.RawMessage `json:"dict"`
			} `json:"stream"`
		}
		if json.Unmarshal(raw, &obj) != nil {
			continue
		}
		dict := obj.Value
		if dict == nil {
			dict = obj.Stream.Dict
		}
		var typ, base, toUnicode string
		if json.Unmarshal(dict["/Type"], &typ) != nil || typ != "/Font" ||
			json.Unmarshal(dict["/BaseFont"], &base) != nil || json.Unmarshal(dict["/ToUnicode"], &toUnicode) != nil {
			continue
		}
		num, _, ok := strings.Cut(toUnicode, " ")
		if !ok || strings.TrimLeft(num, "0123456789") != "" {
			continue
		}
		name := stripSubsetTag(decodeName(strings.TrimPrefix(base, "/")))
		faces[num] = append(faces[num], name)
	}
	if len(faces) == 0 {
		return nil, nil
	}
	streams, err := qpdfObjects(ctx, qpdf, path, "inline", slices.Sorted(maps.Keys(faces)))
	if err != nil {
		return nil, err
	}
	out := make(map[string][]rune)
	for key, raw := range streams {
		num := strings.TrimSuffix(strings.TrimPrefix(key, "obj:"), " 0 R")
		var obj struct {
			Stream struct {
				Data []byte `json:"data"`
			} `json:"stream"`
		}
		if json.Unmarshal(raw, &obj) != nil {
			continue
		}
		runes := toUnicodeRunes(obj.Stream.Data)
		for _, name := range faces[num] {
			out[name] = append(out[name], runes...)
		}
	}
	return out, nil
}

// qpdfObjects returns the objects of the PDF at path, as qpdf --json writes
// them, keyed "obj:N G R": all of them, or the ones of nums, with their
// stream data written as data says ("none" or "inline", decoded).
func qpdfObjects(ctx context.Context, qpdf, path, data string, nums []string) (map[string]json.RawMessage, error) {
	args := make([]string, 0, 5+len(nums))
	args = append(args, "--json=2", "--json-key=qpdf", "--json-stream-data="+data, "--decode-level=generalized")
	for _, n := range nums {
		args = append(args, "--json-object="+n)
	}
	cmd := exec.CommandContext(ctx, qpdf, append(args, path)...) //nolint:gosec // qpdf from LookPath, path is a provisioned reference
	stdout, err := cmd.Output()
	var exitErr *exec.ExitError
	if err != nil && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 3) {
		return nil, fmt.Errorf("running qpdf --json: %w", err)
	}
	var doc struct {
		QPDF []json.RawMessage `json:"qpdf"`
	}
	if err := json.Unmarshal(stdout, &doc); err != nil {
		return nil, fmt.Errorf("reading qpdf --json: %w", err)
	}
	if len(doc.QPDF) < 2 {
		return nil, errors.New("qpdf --json lists no objects")
	}
	var objects map[string]json.RawMessage
	if err := json.Unmarshal(doc.QPDF[1], &objects); err != nil {
		return nil, fmt.Errorf("reading qpdf --json objects: %w", err)
	}
	return objects, nil
}

// cmapHex matches a hexadecimal string of a CMap (ISO 32000-1, 7.3.4.3) or
// an opening or closing bracket of a bfrange's array.
var cmapHex = regexp.MustCompile(`<([0-9A-Fa-f\s]*)>|\[|\]`)

// toUnicodeRunes returns the characters a ToUnicode CMap maps codes to
// (ISO 32000-1, 9.10.3; Adobe Technical Note #5411): the destinations of
// its bfchar entries and of its bfrange entries, a range's destination
// incremented by one per code past its first, or listed in an array. A
// destination is UTF-16BE, a ligature's several characters included.
func toUnicodeRunes(data []byte) []rune {
	var out []rune
	add := func(hexDst string, offset int) {
		b, err := hex.DecodeString(strings.Join(strings.Fields(hexDst), ""))
		if err != nil || len(b) < 2 || len(b)%2 != 0 {
			return
		}
		units := make([]uint16, len(b)/2)
		for i := range units {
			units[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		}
		units[len(units)-1] += uint16(offset) //nolint:gosec // offset is under maxToUnicodeRange, a UTF-16 code unit's range
		out = append(out, utf16.Decode(units)...)
	}
	text := string(data)
	for _, section := range []string{"bfchar", "bfrange"} {
		rest := text
		for {
			start := strings.Index(rest, "begin"+section)
			if start < 0 {
				break
			}
			rest = rest[start+len("begin"+section):]
			end := strings.Index(rest, "end"+section)
			if end < 0 {
				break
			}
			body := rest[:end]
			rest = rest[end:]
			tokens := cmapHex.FindAllStringSubmatch(body, -1)
			if section == "bfchar" {
				for i := 0; i+1 < len(tokens); i += 2 {
					add(tokens[i+1][1], 0)
				}
				continue
			}
			for i := 0; i+2 < len(tokens); {
				lo, err1 := strconv.ParseUint(strings.Join(strings.Fields(tokens[i][1]), ""), 16, 32)
				hi, err2 := strconv.ParseUint(strings.Join(strings.Fields(tokens[i+1][1]), ""), 16, 32)
				if err1 != nil || err2 != nil || hi < lo || hi-lo >= maxToUnicodeRange {
					break
				}
				if tokens[i+2][0] != "[" {
					for c := range int(hi - lo + 1) { // fits int: under maxToUnicodeRange
						add(tokens[i+2][1], c)
					}
					i += 3
					continue
				}
				j := i + 3
				for ; j < len(tokens) && tokens[j][0] != "]"; j++ {
					add(tokens[j][1], 0)
				}
				i = j + 1
			}
		}
	}
	return out
}
