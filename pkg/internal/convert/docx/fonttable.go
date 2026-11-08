package docx

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// The fallback faces of each class of typeface: what fontconfig
// substitutes for a family it does not have, where LibreOffice runs
// Gotenberg, by the class the font table gives it.
//
// # Corpus evidence
//
//   - docx: ooxmlexport/fdo73550.docx (Segoe UI, roman in the font
//     table) and ooxmlexport/FDO77715.docx (Georgia) print in Noto Serif;
//     ooxmlexport/LinkedTextBoxes.docx (Candara, swiss) in Noto Sans, as
//     the slides reader's fallbackFamily.
const (
	fallbackSerif = "Noto Serif"
	fallbackSans  = "Noto Sans"
	fallbackMono  = "DejaVu Sans Mono"
)

// secondFallback is the face after a fallback face, for a font set
// without it: the DejaVu face of its class.
var secondFallback = map[string]string{fallbackSerif: "DejaVu Serif", fallbackSans: "DejaVu Sans"}

// knownClasses classes the common Office families a font table may not
// describe.
var knownClasses = map[string]string{
	"times new roman": fallbackSerif, "cambria": fallbackSerif, "georgia": fallbackSerif, "garamond": fallbackSerif,
	"book antiqua": fallbackSerif, "palatino linotype": fallbackSerif, "constantia": fallbackSerif, "century": fallbackSerif,
	"century schoolbook": fallbackSerif, "bookman old style": fallbackSerif, "liberation serif": fallbackSerif,
	"courier new": fallbackMono, "consolas": fallbackMono, "lucida console": fallbackMono, "courier": fallbackMono,
	"liberation mono": fallbackMono,
}

// readFontTable reads the font table part's root, w:fonts (ECMA-376 Part
// 1, 17.8.3), into the fallback face of each family: its w:family
// (roman serif, modern monospace, any other sans-serif), else the serif
// style of its PANOSE classification (2 to 10 serif, 11 and up sans).
func (d *document) readFontTable(root *ooxml.Element) {
	d.fontClass = map[string]string{}
	for k, v := range knownClasses {
		d.fontClass[k] = v
	}
	for _, f := range root.Kids() {
		if !f.Is(w, "font") {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(attrOr(f, "name")))
		if name == "" {
			continue
		}
		class := ""
		switch attrOr(f.Child(w, "family"), "val") {
		case "roman":
			class = fallbackSerif
		case "modern":
			class = fallbackMono
		case "swiss", "script", "decorative":
			class = fallbackSans
		}
		if p := attrOr(f.Child(w, "panose1"), "val"); class == "" && len(p) >= 4 && p[:2] == "02" {
			if v, err := strconv.ParseUint(p[2:4], 16, 8); err == nil {
				switch {
				case v >= 2 && v <= 10:
					class = fallbackSerif
				case v >= 11:
					class = fallbackSans
				}
			}
		}
		if class != "" {
			if _, known := knownClasses[name]; !known {
				d.fontClass[name] = class
			}
		}
	}
}

// fallbackFor returns the fallback faces of a family, in the order they
// are tried.
func (d *document) fallbackFor(family string) []string {
	c, ok := d.fontClass[strings.ToLower(family)]
	if !ok {
		c = fallbackSans
	}
	if s := secondFallback[c]; s != "" {
		return []string{c, s}
	}
	return []string{c}
}
