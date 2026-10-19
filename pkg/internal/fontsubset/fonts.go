package fontsubset

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// programKeys are the font descriptor entries that hold an embedded font
// program (ISO 32000-1, 9.9, Table 126).
var programKeys = map[string]bool{"FontFile": true, "FontFile2": true, "FontFile3": true}

// programUser is a font that draws its glyphs from a program: the font
// dictionary the descriptor belongs to, a simple font or a CIDFont, and the
// font a /Font resource names it by, the same simple font or the Type 0 font
// whose descendant the CIDFont is (ISO 32000-1, 9.7.1).
type programUser struct {
	font node
	top  node
}

// programUsers returns every font that draws its glyphs from the program
// object num, or why the pass cannot tell: each slot holding the program must
// be a program entry of a font descriptor, each slot holding such a
// descriptor the /FontDescriptor of a font, and each slot holding a CIDFont an
// element of a Type 0 font's /DescendantFonts. Anything else holding one of
// them could draw the program's glyphs some other way.
func (w *walk) programUsers(num int) ([]programUser, string) {
	var users []programUser
	seen := make(map[loc]bool)
	for _, h := range w.g.holders(loc{obj: num}) {
		if !programKeys[h.key] {
			return nil, fmt.Sprintf("the program is held by /%s", h.key)
		}
		for _, fh := range w.g.holders(h.container) {
			if fh.key != "FontDescriptor" {
				return nil, fmt.Sprintf("its font descriptor is held by /%s", fh.key)
			}
			if seen[fh.container] {
				continue
			}
			seen[fh.container] = true
			font := w.nodeAt(fh.container)
			switch font.name("Subtype") {
			case "CIDFontType0", "CIDFontType2":
				tops, reason := w.descendantOf(font)
				if reason != "" {
					return nil, reason
				}
				for _, top := range tops {
					users = append(users, programUser{font: font, top: top})
				}
			case "Type1", "MMType1", "TrueType":
				users = append(users, programUser{font: font, top: font})
			default:
				return nil, fmt.Sprintf("its font descriptor belongs to a /%s font", font.name("Subtype"))
			}
		}
	}
	return users, ""
}

// descendantOf returns the Type 0 fonts whose /DescendantFonts hold the
// CIDFont font.
func (w *walk) descendantOf(font node) ([]node, string) {
	var tops []node
	for _, h := range w.g.holders(font.at) {
		if _, err := strconv.Atoi(h.key); err != nil {
			return nil, fmt.Sprintf("a CIDFont is held by /%s", h.key)
		}
		for _, ah := range w.g.holders(h.container) {
			if ah.key != "DescendantFonts" {
				return nil, fmt.Sprintf("a CIDFont's array is held by /%s", ah.key)
			}
			top := w.nodeAt(ah.container)
			if top.name("Subtype") != "Type0" {
				return nil, "a CIDFont descends from a font other than Type 0"
			}
			tops = append(tops, top)
		}
	}
	return tops, ""
}

// nodeAt returns the value at l.
func (w *walk) nodeAt(l loc) node {
	n := w.g.root()
	if l.obj != trailerObj {
		n = w.g.object(l.obj)
	}
	if l.path == "" {
		return n
	}
	at := loc{obj: l.obj}
	for _, key := range strings.Split(l.path, pathSep) {
		at = at.slot(key)
		next, ok := w.g.child(n, key)
		if !ok {
			return node{val: ast.PdfNull{}, at: at}
		}
		n = next
	}
	return n
}

// fontSeen returns why the walk cannot prove it read every use of the font
// a /Font resource names, "" when it can: every slot holding the font must be
// an entry of a /Font dictionary the walk read, under a name no default
// appearance string names; every slot holding that /Font dictionary the /Font
// entry of a resource dictionary the walk interpreted every stream against,
// none of which failed to read and none of which a reader regenerates an
// appearance with; and every slot holding that resource dictionary the
// /Resources entry of an object the walk read every stream of.
func (w *walk) fontSeen(font node) string {
	holders := w.g.holders(font.at)
	if len(holders) == 0 {
		return "the font is held by nothing"
	}
	for _, h := range holders {
		if !w.fontDicts[h.container] {
			return fmt.Sprintf("the font is held by /%s outside a /Font resource the walk read", h.key)
		}
		if w.g.daNames[ast.PdfName(h.key)] {
			// A reader filling a field types with the font its default
			// appearance names (ISO 32000-1, 12.7.3.3), glyphs no stream
			// shows.
			return fmt.Sprintf("a default appearance names /%s", h.key)
		}
		for _, rh := range w.g.holders(h.container) {
			if rh.key != "Font" {
				return fmt.Sprintf("a /Font dictionary is held by /%s", rh.key)
			}
			if reason := w.resourcesSeen(rh.container); reason != "" {
				return reason
			}
		}
	}
	return ""
}

// resourcesSeen returns why the walk cannot prove it read every stream that
// resolves names in the resource dictionary at res, "" when it can.
func (w *walk) resourcesSeen(res loc) string {
	switch {
	case !w.resources[res]:
		return "a resource dictionary the walk did not read"
	case w.tainted[res]:
		return "a resource dictionary a stream failed to read against"
	case w.formRes[res]:
		return "the resources of a widget appearance a reader regenerates"
	}
	holders := w.g.holders(res)
	if len(holders) == 0 {
		return "a resource dictionary held by nothing"
	}
	for _, h := range holders {
		if h.key != "Resources" || !w.owners[h.container] {
			return fmt.Sprintf("a resource dictionary held by /%s of an object the walk did not read", h.key)
		}
	}
	return ""
}
