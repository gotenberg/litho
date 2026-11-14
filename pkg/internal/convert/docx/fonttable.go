package docx

import (
	"maps"
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
//   - libreoffice/docx: ooxmlexport/fdo73550.docx (Segoe UI, roman in the font
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
// style of its PANOSE classification (2 to 10 serif, 11 and up sans);
// and into the alternate name of each family that states one (17.8.3.1
// altName), which [document.fallbackFor] tries first.
func (d *document) readFontTable(root *ooxml.Element) {
	d.fontClass = map[string]string{}
	d.fontAlt = map[string]string{}
	maps.Copy(d.fontClass, knownClasses)
	for _, f := range root.Kids() {
		if !f.Is(w, "font") {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(attrOr(f, "name")))
		if name == "" {
			continue
		}
		if alt := strings.TrimSpace(attrOr(f.Child(w, "altName"), "val")); alt != "" && !strings.EqualFold(alt, name) && len(d.fontAlt) < maxFontAlts {
			d.fontAlt[name] = alt
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

// maxFontAlts bounds the alternate names read from a font table.
// Anti-DoS guard, not spec-mandated: a document names a few dozen fonts.
const maxFontAlts = 1024

// fallbackFor returns the fallback faces of a family, in the order they
// are tried: the alternate name the font table gives it, as Word prints a
// family it does not have in the face its w:altName names, then the
// fallback faces of its class.
//
// # Corpus evidence
//
//   - libreoffice/docx: ooxmlexport/TextFrameRotation.docx prints its
//     Kings Caslon Text, altName Corbel, in Corbel; ooxmlexport/fdo38414.docx
//     its TimesCE, altName Times New Roman, in Times New Roman;
//     ooxmlexport/FDO77715.docx its Chaparral Pro, altName Cambria, in
//     Cambria; ooxmlexport/content-control-grab-bag.docx its Frutiger
//     Light, altName Segoe UI Semilight, in Segoe UI Semilight.
//   - poi/docx: document/57312.docx prints Switzerland, altName Times New
//     Roman, and document/headerPic.docx Baltica and FuturisXCTT, the
//     same, in Times New Roman; document/59378.docx Swis721 BT, altName
//     Arial, in Arial.
//   - openxmlsdk/docx: complexDocx/complex_tables.docx prints Microsoft
//     Head, altName Arial, in Arial.
func (d *document) fallbackFor(family string) []string {
	key := strings.ToLower(family)
	c, ok := d.fontClass[key]
	if !ok {
		c = fallbackSans
	}
	out := make([]string, 0, 3)
	if alt := d.fontAlt[key]; alt != "" {
		out = append(out, alt)
	}
	out = append(out, c)
	if s := secondFallback[c]; s != "" {
		out = append(out, s)
	}
	return out
}
