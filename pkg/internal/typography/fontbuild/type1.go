package fontbuild

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"slices"
)

// Type1 describes a Type 1 program as a /FontFile stream embeds it: a
// clear-text portion, a binary eexec-encrypted portion and a trailer of
// zeros and cleartomark (Adobe Type 1 Font Format, 2.3 and 7.2).
type Type1 struct {
	FontName string
	// Glyphs follow the .notdef glyph, which the program's /CharStrings
	// dictionary always holds first, with the advance of NotdefAdvance.
	// Their Unicode, Symbol and Components fields are ignored: a Type 1
	// program reaches a glyph by name, and Seac builds an accented glyph.
	Glyphs        []Glyph
	NotdefAdvance uint16
	// Encoding, when set, is the program's encoding vector mapping codes to
	// glyph names. Unset, the program names the Standard encoding.
	Encoding map[byte]string
	// Subrs draws every glyph's outline through subroutine 4, after the four
	// subroutines the hint replacement and flex conventions reserve.
	Subrs bool
	// NoTrailer leaves the trailer out, as some producers do, so the program
	// ends with its encrypted portion.
	NoTrailer bool
}

// Type 1 cipher constants (Adobe Type 1 Font Format, 7.1).
const (
	t1EexecKey      = 55665
	t1CharStringKey = 4330
	t1C1            = 52845
	t1C2            = 22719
	t1LenIV         = 4
)

// Build returns the program's bytes and the lengths of its clear-text,
// encrypted and trailer portions, as a /FontFile stream's /Length1, /Length2
// and /Length3 give them.
func (t Type1) Build() ([]byte, [3]int) {
	glyphs := append([]Glyph{{Name: ".notdef", Advance: t.NotdefAdvance, Empty: true}}, t.Glyphs...)

	var clear bytes.Buffer
	fmt.Fprintf(&clear, "%%!PS-AdobeFont-1.0: %s 001.000\n", t.FontName)
	clear.WriteString("12 dict begin\n")
	fmt.Fprintf(&clear, "/FontName /%s def\n", t.FontName)
	clear.WriteString("/PaintType 0 def\n/FontType 1 def\n")
	clear.WriteString("/FontMatrix [0.001 0 0 0.001 0 0] readonly def\n")
	clear.WriteString("/FontBBox {0 0 1000 1000} readonly def\n")
	if t.Encoding == nil {
		clear.WriteString("/Encoding StandardEncoding def\n")
	} else {
		clear.WriteString("/Encoding 256 array\n0 1 255 {1 index exch /.notdef put} for\n")
		for _, c := range slices.Sorted(maps.Keys(t.Encoding)) {
			fmt.Fprintf(&clear, "dup %d /%s put\n", c, t.Encoding[c])
		}
		clear.WriteString("readonly def\n")
	}
	clear.WriteString("currentfile eexec\n")

	var priv bytes.Buffer
	priv.Write([]byte{0, 0, 0, 0}) // eexec salt
	priv.WriteString("dup /Private 8 dict dup begin\n")
	priv.WriteString("/RD{string currentfile exch readstring pop}executeonly def\n")
	priv.WriteString("/ND{noaccess def}executeonly def\n")
	priv.WriteString("/NP{noaccess put}executeonly def\n")
	priv.WriteString("/lenIV 4 def\n/MinFeature{16 16} def\n/password 5839 def\n/BlueValues [] def\n")
	if t.Subrs {
		subrs := [][]byte{{11}, {11}, {11}, {11}, append(t1Outline(), 11)}
		fmt.Fprintf(&priv, "/Subrs %d array\n", len(subrs))
		for i, s := range subrs {
			enc := t1Crypt(append(make([]byte, t1LenIV), s...), t1CharStringKey)
			fmt.Fprintf(&priv, "dup %d %d RD ", i, len(enc))
			priv.Write(enc)
			priv.WriteString(" NP\n")
		}
		priv.WriteString("ND\n")
	}
	fmt.Fprintf(&priv, "2 index /CharStrings %d dict dup begin\n", len(glyphs))
	for _, g := range glyphs {
		cs := append(t1Int(0), t1Int(int32(g.Advance))...)
		cs = append(cs, 13) // hsbw
		switch {
		case g.Seac != nil:
			for _, v := range []int32{0, 0, 0, int32(g.Seac[0]), int32(g.Seac[1])} {
				cs = append(cs, t1Int(v)...)
			}
			cs = append(cs, 12, 6) // seac
		case g.Empty:
			cs = append(cs, 14) // endchar
		case t.Subrs:
			cs = append(cs, t1Int(4)...)
			cs = append(cs, 10, 14) // callsubr, endchar
		default:
			cs = append(cs, t1Outline()...)
			cs = append(cs, 14)
		}
		enc := t1Crypt(append(make([]byte, t1LenIV), cs...), t1CharStringKey)
		fmt.Fprintf(&priv, "/%s %d RD ", g.Name, len(enc))
		priv.Write(enc)
		priv.WriteString(" ND\n")
	}
	priv.WriteString("end\n")
	priv.WriteString("end\nreadonly put\nnoaccess put\n")
	priv.WriteString("dup /FontName get exch definefont pop\nmark currentfile closefile\n")
	encrypted := t1Crypt(priv.Bytes(), t1EexecKey)

	var trailer bytes.Buffer
	if !t.NoTrailer {
		for range 8 {
			trailer.Write(bytes.Repeat([]byte("0"), 64))
			trailer.WriteByte('\n')
		}
		trailer.WriteString("cleartomark\n")
	}

	out := append(clear.Bytes(), encrypted...)
	out = append(out, trailer.Bytes()...)
	return out, [3]int{clear.Len(), len(encrypted), trailer.Len()}
}

// t1Outline returns the charstring operators drawing a square from the
// origin: rmoveto, two rlinetos and closepath.
func t1Outline() []byte {
	cs := make([]byte, 0, 16)
	cs = append(cs, t1Int(0)...)
	cs = append(cs, t1Int(0)...)
	cs = append(cs, 21) // rmoveto
	cs = append(cs, t1Int(500)...)
	cs = append(cs, t1Int(0)...)
	cs = append(cs, 5) // rlineto
	cs = append(cs, t1Int(0)...)
	cs = append(cs, t1Int(500)...)
	cs = append(cs, 5, 9) // rlineto, closepath
	return cs
}

// t1Int encodes v as a Type 1 charstring number (Adobe Type 1 Font Format,
// 6.2).
func t1Int(v int32) []byte {
	switch {
	case v >= -107 && v <= 107:
		return []byte{byte(v + 139)}
	case v >= 108 && v <= 1131:
		v -= 108
		return []byte{byte(v/256 + 247), byte(v % 256)}
	case v >= -1131 && v <= -108:
		v = -v - 108
		return []byte{byte(v/256 + 251), byte(v % 256)}
	}
	out := []byte{255, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(out[1:], uint32(v)) //nolint:gosec // G115: two's-complement bytes of an int32.
	return out
}

// t1Crypt encrypts plain with the Type 1 cipher under key r (Adobe Type 1
// Font Format, 7.1).
func t1Crypt(plain []byte, r uint32) []byte {
	out := make([]byte, len(plain))
	for i, p := range plain {
		c := p ^ byte(r>>8) //nolint:gosec // G115: keeps bits 8 to 15 of the key, the high byte of the 16-bit r.
		out[i] = c
		r = (uint32(c)+r)*t1C1 + t1C2
	}
	return out
}
