package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Where the definitions come from: the archive of ECMA-376 Part 1, fifth
// edition (December 2016), the annex archive inside it, the definitions
// file inside that, and the digests of the archive and of the file.
const (
	archiveURL    = "https://ecma-international.org/wp-content/uploads/ECMA-376-1_5th_edition_december_2016.zip"
	archiveSHA256 = "9d0bcad9cf06054785b03762fcfadbf6bab7e54a5f9d69434e34b7fd464d4129"
	annexName     = "OfficeOpenXML-DrawingMLGeometries.zip"
	defsName      = "presetTextWarpDefinitions.xml"
	defsSHA256    = "89640bd4aeedfa2c578d1d1acc50d569865ebdef315fd4b468087c25e36acada"
	outGoName     = "textwarps_gen.go"
	warpsCount    = 40
)

// The caps on what the generator reads. Anti-DoS guards for a build-time
// tool, not format limits: the archive is 43,501,721 bytes, the annex
// archive 51,672 and the definitions 51,599.
const (
	maxArchiveBytes = 64 << 20
	maxDefsBytes    = 1 << 20
)

// httpClient bounds how long the generator waits for the download.
var httpClient = &http.Client{Timeout: 10 * time.Minute}

func main() {
	src := flag.String("src", "", "local copy of presetTextWarpDefinitions.xml, instead of downloading the archive")
	out := flag.String("out", outGoName, "Go file to write")
	flag.Parse()
	if err := run(*src, *out); err != nil {
		fmt.Fprintln(os.Stderr, "gen-text-warps:", err)
		os.Exit(1)
	}
}

// run reads the definitions and writes the Go file.
func run(src, out string) error {
	defs, err := readDefs(src)
	if err != nil {
		return err
	}
	if got := digest(defs); got != defsSHA256 {
		return fmt.Errorf("definitions digest %s, want %s", got, defsSHA256)
	}
	code, err := generate(defs)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Clean(out), code, 0o600)
}

// digest returns the hexadecimal SHA-256 of b.
func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// readDefs returns the definitions from src, or from the downloaded
// archive.
func readDefs(src string) ([]byte, error) {
	if src != "" {
		return os.ReadFile(filepath.Clean(src))
	}
	resp, err := httpClient.Get(archiveURL)
	if err != nil {
		return nil, fmt.Errorf("downloading the archive: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // a read-only response body has nothing to flush
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading the archive: %s", resp.Status)
	}
	archive, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveBytes+1))
	if err != nil {
		return nil, fmt.Errorf("downloading the archive: %w", err)
	}
	if len(archive) > maxArchiveBytes {
		return nil, errors.New("the archive is larger than expected")
	}
	if got := digest(archive); got != archiveSHA256 {
		return nil, fmt.Errorf("archive digest %s, want %s", got, archiveSHA256)
	}
	annex, err := member(archive, annexName, maxArchiveBytes)
	if err != nil {
		return nil, err
	}
	return member(annex, defsName, maxDefsBytes)
}

// member returns the file name of the zip archive data, at most limit
// bytes.
func member(data []byte, name string, limit int64) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("reading the archive holding %s: %w", name, err)
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("opening %s: %w", name, err)
		}
		b, err := io.ReadAll(io.LimitReader(rc, limit+1))
		cerr := rc.Close()
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		if cerr != nil {
			return nil, fmt.Errorf("closing %s: %w", name, cerr)
		}
		if int64(len(b)) > limit {
			return nil, fmt.Errorf("%s is larger than expected", name)
		}
		return b, nil
	}
	return nil, fmt.Errorf("no %s in the archive", name)
}

// node is one element of the definitions file.
type node struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Kids    []node     `xml:",any"`
}

// attr returns the attribute name of n, "" when absent.
func (n *node) attr(name string) string {
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// kid returns the first child named name, nil for none.
func (n *node) kid(name string) *node {
	for i := range n.Kids {
		if n.Kids[i].XMLName.Local == name {
			return &n.Kids[i]
		}
	}
	return nil
}

// The operators of ECMA-376 Part 1, 20.1.9.11, with the shape package's
// names for them and their operand counts.
var operators = map[string]struct {
	ident string
	n     int
}{
	"val": {"OpVal", 1}, "*/": {"OpMulDiv", 3}, "+-": {"OpAddSub", 3}, "+/": {"OpAddDiv", 3},
	"?:": {"OpIfElse", 3}, "abs": {"OpAbs", 1}, "at2": {"OpAt2", 2}, "cat2": {"OpCat2", 3},
	"cos": {"OpCos", 2}, "max": {"OpMax", 2}, "min": {"OpMin", 2}, "mod": {"OpMod", 3},
	"pin": {"OpPin", 3}, "sat2": {"OpSat2", 3}, "sin": {"OpSin", 2}, "sqrt": {"OpSqrt", 1},
	"tan": {"OpTan", 2},
}

// builtins are the built-in guide names of ECMA-376 Part 1, 20.1.10.76;
// the shape package names each Builtin plus the name in capitals.
var builtins = map[string]bool{
	"l": true, "t": true, "r": true, "b": true, "w": true, "h": true, "hc": true, "vc": true,
	"ss": true, "ls": true, "cd2": true, "cd4": true, "cd8": true, "3cd4": true, "3cd8": true,
	"5cd8": true, "7cd8": true, "wd2": true, "wd3": true, "wd4": true, "wd5": true, "wd6": true,
	"wd8": true, "wd10": true, "wd12": true, "wd32": true, "hd2": true, "hd3": true, "hd4": true,
	"hd5": true, "hd6": true, "hd8": true, "ssd2": true, "ssd4": true, "ssd6": true, "ssd8": true,
	"ssd16": true, "ssd32": true,
}

// literalRe matches an integer operand (20.1.10.3 ST_AdjCoordinate).
var literalRe = regexp.MustCompile(`^-?[0-9]+$`)

// warp is one definition being written.
type warp struct {
	names []string
}

// operand returns the Go expression of the operand s seen from guide at:
// a literal, the latest earlier guide of that name, or a built-in. A
// preset names nothing else.
func (p *warp) operand(s string, at int) (string, error) {
	if literalRe.MatchString(s) {
		return "lit(" + s + ")", nil
	}
	for i := min(at, len(p.names)) - 1; i >= 0; i-- {
		if p.names[i] == s {
			return "ref(" + strconv.Itoa(i) + ")", nil
		}
	}
	if builtins[s] {
		return "bin(Builtin" + strings.ToUpper(s) + ")", nil
	}
	return "", fmt.Errorf("unresolved name %q", s)
}

// header opens the generated file.
const header = `// Code generated by gen-text-warps; DO NOT EDIT.

package shape

// textWarpTable holds the preset text warps of ECMA-376 Part 1, Annex D
// (presetTextWarpDefinitions.xml in OfficeOpenXML-DrawingMLGeometries.zip
// of the standard's fifth edition, December 2016), by their 20.1.10.76
// ST_TextShapeType name.
//
// The definitions are © Ecma International. Ecma publishes them free of
// charge under its copyright notice, which allows derivative works that
// implement the standard provided the notice is kept.
var textWarpTable = map[string]*Custom{
`

// generate reads the definitions defs and returns the Go file.
func generate(defs []byte) ([]byte, error) {
	var root node
	if err := xml.Unmarshal(bytes.TrimPrefix(defs, []byte("\xef\xbb\xbf")), &root); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", defsName, err)
	}
	var b strings.Builder
	b.WriteString(header)
	seen := map[string]bool{}
	for i := range root.Kids {
		name := root.Kids[i].XMLName.Local
		if seen[name] {
			return nil, fmt.Errorf("preset %s defined twice", name)
		}
		seen[name] = true
		if err := writeWarp(&b, &root.Kids[i]); err != nil {
			return nil, fmt.Errorf("preset %s: %w", name, err)
		}
	}
	b.WriteString("}\n")
	if len(root.Kids) != warpsCount {
		return nil, fmt.Errorf("%d presets, want %d", len(root.Kids), warpsCount)
	}
	return format.Source([]byte(b.String()))
}

// writeWarp writes one table entry.
func writeWarp(b *strings.Builder, n *node) error {
	p := &warp{}
	type guide struct{ name, fmla string }
	var lists [2][]guide
	for li, list := range []string{"avLst", "gdLst"} {
		if l := n.kid(list); l != nil {
			for _, g := range l.Kids {
				lists[li] = append(lists[li], guide{g.attr("name"), g.attr("fmla")})
				p.names = append(p.names, g.attr("name"))
			}
		}
	}
	if len(p.names) > maxGuides {
		return fmt.Errorf("%d guides, past the cap of %d", len(p.names), maxGuides)
	}
	fmt.Fprintf(b, "%q: {\n", n.XMLName.Local)
	at := 0
	for li, field := range []string{"Adjust", "Guides"} {
		if len(lists[li]) == 0 {
			continue
		}
		fmt.Fprintf(b, "%s: []Guide{\n", field)
		for _, g := range lists[li] {
			f := strings.Fields(g.fmla)
			if len(f) == 0 {
				return fmt.Errorf("guide %s has no formula", g.name)
			}
			op, ok := operators[f[0]]
			if !ok || len(f) != op.n+1 {
				return fmt.Errorf("guide %s: formula %q", g.name, g.fmla)
			}
			fmt.Fprintf(b, "gd(%q, %s", g.name, op.ident)
			for _, s := range f[1:] {
				e, err := p.operand(s, at)
				if err != nil {
					return fmt.Errorf("guide %s: %w", g.name, err)
				}
				b.WriteString(", " + e)
			}
			b.WriteString("),\n")
			at++
		}
		b.WriteString("},\n")
	}
	all := len(p.names)
	b.WriteString("Paths: []PathDef{\n")
	cmds := 0
	pl := n.kid("pathLst")
	if pl == nil {
		return errors.New("no pathLst")
	}
	for _, path := range pl.Kids {
		b.WriteString("{Stroke: true, Cmds: []Cmd{\n")
		for _, c := range path.Kids {
			cmds++
			if err := writeCommand(b, p, &c, all); err != nil {
				return err
			}
		}
		b.WriteString("}},\n")
	}
	if len(pl.Kids) > maxPaths || cmds > maxCommands {
		return fmt.Errorf("%d paths and %d commands, past the caps", len(pl.Kids), cmds)
	}
	b.WriteString("},\n},\n")
	return nil
}

// The caps of the shape package a preset must fit (shape.MaxGuides,
// MaxPaths, MaxCommands).
const (
	maxGuides   = 4096
	maxPaths    = 1024
	maxCommands = 1 << 16
)

// writeCommand writes one path command (20.1.9.15).
func writeCommand(b *strings.Builder, p *warp, c *node, all int) error {
	var kind string
	var ops []string
	switch c.XMLName.Local {
	case "moveTo", "lnTo", "quadBezTo", "cubicBezTo":
		kind = map[string]string{"moveTo": "CmdMove", "lnTo": "CmdLine", "quadBezTo": "CmdQuad", "cubicBezTo": "CmdCubic"}[c.XMLName.Local]
		for _, pt := range c.Kids {
			ops = append(ops, pt.attr("x"), pt.attr("y"))
		}
	case "arcTo":
		kind = "CmdArc"
		ops = []string{c.attr("wR"), c.attr("hR"), c.attr("stAng"), c.attr("swAng")}
	case "close":
		kind = "CmdClose"
	default:
		return fmt.Errorf("unknown path command %s", c.XMLName.Local)
	}
	b.WriteString("cmd(" + kind)
	for _, s := range ops {
		e, err := p.operand(s, all)
		if err != nil {
			return fmt.Errorf("%s: %w", c.XMLName.Local, err)
		}
		b.WriteString(", " + e)
	}
	b.WriteString("),\n")
	return nil
}
