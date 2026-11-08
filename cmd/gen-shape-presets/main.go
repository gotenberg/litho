package main

import (
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

// The definitions the presets come from: LibreOffice core's copy of
// ECMA-376 Part 1, Annex D's presetShapeDefinitions.xml at the commit the
// corpora pin, and the digest of the file that commit holds. It is the
// annex's file with three fixes LibreOffice made: the annex lists
// upDownArrow twice and upArrow not at all, names a guide of noSmoking
// "2dang", which 20.1.10.3 ST_GeomGuideName forbids, and bounds the
// handles of star24 and star32 by ssd2 instead of 50000.
const (
	defsURL      = "https://raw.githubusercontent.com/LibreOffice/core/9dea406ab7695df6325b11b0e19fdca8db4b7276/oox/source/drawingml/customshapes/presetShapeDefinitions.xml"
	defsSHA256   = "eaff19f4405b3be6822428c96ef46cc685499217366fd58e5e63a5a24739f02f"
	presetName   = "presetShapeDefinitions.xml"
	outGoName    = "presets_gen.go"
	presetsCount = 187
)

// maxDefsBytes caps the downloaded file. Anti-DoS guard for a build-time
// tool, not a format limit: the file is 538,972 bytes.
const maxDefsBytes = 8 << 20

// httpClient bounds how long the generator waits for the download.
var httpClient = &http.Client{Timeout: 5 * time.Minute}

func main() {
	src := flag.String("src", "", "local copy of presetShapeDefinitions.xml, instead of downloading it")
	out := flag.String("out", outGoName, "Go file to write")
	flag.Parse()
	if err := run(*src, *out); err != nil {
		fmt.Fprintln(os.Stderr, "gen-shape-presets:", err)
		os.Exit(1)
	}
}

// run reads the definitions and writes the Go file.
func run(src, out string) error {
	defs, err := readDefs(src)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(defs)
	if got := hex.EncodeToString(sum[:]); got != defsSHA256 {
		return fmt.Errorf("definitions digest %s, want %s", got, defsSHA256)
	}
	code, err := generate(defs)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Clean(out), code, 0o600)
}

// readDefs returns the definitions from src, or downloaded.
func readDefs(src string) ([]byte, error) {
	if src != "" {
		return os.ReadFile(filepath.Clean(src))
	}
	resp, err := httpClient.Get(defsURL)
	if err != nil {
		return nil, fmt.Errorf("downloading the definitions: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // a read-only response body has nothing to flush
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading the definitions: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDefsBytes+1))
	if err != nil {
		return nil, fmt.Errorf("downloading the definitions: %w", err)
	}
	if len(data) > maxDefsBytes {
		return nil, errors.New("the definitions are larger than expected")
	}
	return data, nil
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

// builtins are the built-in guide names of ECMA-376 Part 1, 20.1.9.11;
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

// preset is one definition being written.
type preset struct {
	names []string
}

// operand returns the Go expression of the operand s seen from guide at:
// a literal, the latest earlier guide of that name, or a built-in. A
// preset names nothing else.
func (p *preset) operand(s string, at int) (string, error) {
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

// generate reads the preset definitions defs and returns the Go file.
func generate(defs []byte) ([]byte, error) {
	var root node
	if err := xml.Unmarshal(bytes.TrimPrefix(defs, []byte("\xef\xbb\xbf")), &root); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", presetName, err)
	}
	var b strings.Builder
	b.WriteString("// Code generated by gen-shape-presets; DO NOT EDIT.\n\npackage shape\n\n")
	b.WriteString("// presetTable holds the preset geometries of ECMA-376 Part 1, Annex D\n")
	b.WriteString("// (presetShapeDefinitions.xml), by their 20.1.10.56 ST_ShapeType name.\n")
	b.WriteString("var presetTable = map[string]*Custom{ //nolint:gochecknoglobals // a read-only spec table, ECMA-376 Part 1, Annex D.\n")
	seen := map[string]bool{}
	for i := range root.Kids {
		if name := root.Kids[i].XMLName.Local; seen[name] {
			return nil, fmt.Errorf("preset %s defined twice", name)
		}
		seen[root.Kids[i].XMLName.Local] = true
		if err := writePreset(&b, &root.Kids[i]); err != nil {
			return nil, fmt.Errorf("preset %s: %w", root.Kids[i].XMLName.Local, err)
		}
	}
	b.WriteString("}\n")
	if len(root.Kids) != presetsCount {
		return nil, fmt.Errorf("%d presets, want %d", len(root.Kids), presetsCount)
	}
	return format.Source([]byte(b.String()))
}

// writePreset writes one table entry.
func writePreset(b *strings.Builder, n *node) error {
	p := &preset{}
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
			if !ok || len(f) < op.n+1 {
				return fmt.Errorf("guide %s: formula %q", g.name, g.fmla)
			}
			// circularArrow's xB carries a fourth operand ("+- xH 0 dxB
			// 0"); the operator reads its first three, as LibreOffice does.
			fmt.Fprintf(b, "gd(%q, %s", g.name, op.ident)
			for _, s := range f[1 : op.n+1] {
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
	if r := n.kid("rect"); r != nil {
		var e [4]string
		for i, k := range []string{"l", "t", "r", "b"} {
			v, err := p.operand(r.attr(k), all)
			if err != nil {
				return fmt.Errorf("rect: %w", err)
			}
			e[i] = v
		}
		fmt.Fprintf(b, "Text: &TextBox{L: %s, T: %s, R: %s, B: %s},\n", e[0], e[1], e[2], e[3])
	}
	b.WriteString("Paths: []PathDef{\n")
	cmds := 0
	pl := n.kid("pathLst")
	if pl == nil {
		return errors.New("no pathLst")
	}
	for _, path := range pl.Kids {
		b.WriteString("{")
		for _, k := range []string{"w", "h"} {
			if v := path.attr(k); v != "" {
				fmt.Fprintf(b, "%s: %s, ", strings.ToUpper(k), v)
			}
		}
		if f, ok := fillModes[path.attr("fill")]; ok {
			fmt.Fprintf(b, "Fill: %s, ", f)
		}
		if path.attr("stroke") != "false" && path.attr("stroke") != "0" {
			b.WriteString("Stroke: true, ")
		}
		b.WriteString("Cmds: []Cmd{\n")
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

// fillModes maps 20.1.10.37 ST_PathFillMode to the shape package's names;
// norm, the default, is left out.
var fillModes = map[string]string{
	"none": "FillNone", "lighten": "FillLighten", "lightenLess": "FillLightenLess",
	"darken": "FillDarken", "darkenLess": "FillDarkenLess",
}

// writeCommand writes one path command (20.1.9.15).
func writeCommand(b *strings.Builder, p *preset, c *node, all int) error {
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
