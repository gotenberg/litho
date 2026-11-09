package build

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestHelpersMatchPython pins the ports of truetype.go, cff.go, type1.go,
// icc.go and jp2.go byte for byte: testdata/helpers_golden.txt holds the
// length and SHA-256 of each case as the Python modules emit it.
func TestHelpersMatchPython(t *testing.T) {
	t.Parallel()

	golden := helpersGolden(t)
	cases := helpersCases()
	for name := range golden {
		if _, ok := cases[name]; !ok {
			t.Errorf("%s: golden case has no Go counterpart", name)
		}
	}
	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		want, ok := golden[name]
		if !ok {
			t.Errorf("%s: no golden entry", name)
			continue
		}
		got := cases[name]()
		sum := sha256.Sum256(got)
		if len(got) != want.length || hex.EncodeToString(sum[:]) != want.sha {
			t.Errorf("%s: got %d bytes %x, want %d bytes %s", name, len(got), sum, want.length, want.sha)
		}
	}
}

type helpersEntry struct {
	length int
	sha    string
}

func helpersGolden(t *testing.T) map[string]helpersEntry {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "helpers_golden.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]helpersEntry{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			t.Fatalf("malformed golden line %q", line)
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil {
			t.Fatal(err)
		}
		out[fields[0]] = helpersEntry{n, fields[2]}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func helpersT1(program []byte, l1, l2, l3 int) []byte {
	out := bytes.Clone(program)
	for _, v := range []int{l1, l2, l3} {
		out = binary.BigEndian.AppendUint32(out, uint32(v)) //nolint:gosec // lengths of small programs.
	}
	return out
}

func helpersCat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

func helpersCases() map[string]func() []byte {
	latin := map[int]string{0x41: "A", 0x42: "B"}
	sym := map[int]string{0xF041: "A", 0xF042: "B"}
	merged := map[int]string{0x41: "A", 0x42: "B", 0xF041: "A", 0xF042: "B"}
	names := []string{".notdef", "Adieresis", "adieresis"}
	unicode := map[int]string{0xC4: "Adieresis", 0xE4: "adieresis"}
	mac := map[int]string{0x80: "Adieresis", 0x8A: "adieresis"}
	wide := map[int]string{}
	for c := 0x20; c < 0x7F; c++ {
		if c%2 != 0 {
			wide[c] = "A"
		} else {
			wide[c] = "B"
		}
	}
	full := func() []byte { return ttBuild([]ttSubtable{{3, 1, latin}}, nil) }
	letters := func(lines ...string) []string {
		set := map[string]bool{}
		for _, l := range lines {
			for _, c := range []byte(l) {
				if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
					set[string(rune(c))] = true
				}
			}
		}
		out := make([]string, 0, len(set))
		for k := range set {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	ab := map[int]string{65: "A", 66: "B"}
	srgb := jp2EnumSRGB

	cases := map[string]func() []byte{
		"tt.latin31_10": func() []byte { return ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil) },
		"tt.latin10_31": func() []byte { return ttBuild([]ttSubtable{{1, 0, latin}, {3, 1, latin}}, nil) },
		"tt.latin31":    full,
		"tt.latin10":    func() []byte { return ttBuild([]ttSubtable{{1, 0, latin}}, nil) },
		"tt.sym30":      func() []byte { return ttBuild([]ttSubtable{{3, 0, sym}}, nil) },
		"tt.merged30":   func() []byte { return ttBuild([]ttSubtable{{3, 0, merged}}, nil) },
		"tt.empty":      func() []byte { return ttBuild(nil, nil) },
		"tt.symbolname": func() []byte {
			return ttBuild([]ttSubtable{{3, 1, map[int]string{0x41: "A", 0x42: "H20850"}}}, []string{".notdef", "A", "H20850"})
		},
		"tt.maccopy":    func() []byte { return ttBuild([]ttSubtable{{1, 0, unicode}, {3, 1, unicode}}, names) },
		"tt.macgenuine": func() []byte { return ttBuild([]ttSubtable{{1, 0, mac}, {3, 1, unicode}}, names) },
		"tt.wide3": func() []byte {
			return ttBuild([]ttSubtable{{3, 1, wide}, {1, 0, wide}, {0, 3, map[int]string{0x41: ".notdef", 0x42: "B"}}}, nil)
		},
		"tt.assemble_otto": func() []byte { return ttAssemble([]byte("OTTO"), ttTablesOf(full())) },
		"tt.assemble_true": func() []byte { return ttAssemble([]byte{0, 1, 0, 0}, ttTablesOf(full())) },
		"tt.assemble_cff2": func() []byte {
			t := ttTablesOf(full())
			delete(t, "glyf")
			delete(t, "loca")
			t["maxp"] = binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint32(nil, 0x00005000), uint16(len(ttGlyphNames)))
			t["CFF2"] = cffBuildCFF2()
			var hmtx []byte
			for range ttGlyphNames {
				hmtx = binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(hmtx, 1000), 50)
			}
			t["hmtx"] = hmtx
			return ttAssemble([]byte("OTTO"), t)
		},
		"tt.assemble_empty": func() []byte { return ttAssemble([]byte("true"), map[string][]byte{}) },
		"tt.tables_concat": func() []byte {
			t := ttTablesOf(full())
			var out []byte
			for _, k := range ttSortedTags(t) {
				out = append(out, k...)
				out = binary.BigEndian.AppendUint32(out, uint32(len(t[k]))) //nolint:gosec // small tables.
				out = append(out, t[k]...)
			}
			return out
		},
		"tt.checksum": func() []byte {
			var out []byte
			for _, d := range [][]byte{{}, {1}, []byte("abcde"), bytes.Repeat([]byte{0xff}, 13), full()} {
				out = binary.BigEndian.AppendUint32(out, ttChecksum(d))
			}
			return out
		},
		"tt.glyf_rect":    func() []byte { return helpersCat(ttGlyfRect(700), ttGlyfRect(3)) },
		"tt.cmap_format4": func() []byte { return ttCmapFormat4(map[int]int{0x41: 1, 0x42: 2, 0x20: 0}) },
		"tt.cmap": func() []byte {
			return ttCmap([]ttResolvedSubtable{{3, 1, map[int]int{0x41: 1}}, {1, 0, map[int]int{0x41: 2, 0x42: 1}}})
		},
		"tt.name":       ttName,
		"tt.post":       func() []byte { return ttPost([]string{".notdef", "A", "B", "longername"}) },
		"cff.build":     cffBuild,
		"cff.cid12":     func() []byte { return cffBuildCID([]int{1, 2}) },
		"cff.cid123":    func() []byte { return cffBuildCID([]int{1, 2, 3}) },
		"cff.cid_empty": func() []byte { return cffBuildCID(nil) },
		"cff.cid_big": func() []byte {
			var cids []int
			for i := 1; i < 40; i++ {
				cids = append(cids, i)
			}
			return cffBuildCID(cids)
		},
		"cff.cs_empty700": func() []byte { return cffBuildCharstrings(nil, 700) },
		"cff.cs_three600": func() []byte {
			return cffBuildCharstrings([][]byte{{0x0e}, cffRectCharstring(400), cffRectCharstring(500)}, cffAdvanceWidth)
		},
		"cff.cs_one":       func() []byte { return cffBuildCharstrings([][]byte{{0x0e}}, 650) },
		"cff.cidcs_0_700":  func() []byte { return cffBuildCIDCharstrings(0, 700) },
		"cff.cidcs_1_700":  func() []byte { return cffBuildCIDCharstrings(1, 700) },
		"cff.cidcs_2_600":  func() []byte { return cffBuildCIDCharstrings(2, 600) },
		"cff.cidcs_3_600":  func() []byte { return cffBuildCIDCharstrings(3, cffAdvanceWidth) },
		"cff.cidcs_5_600":  func() []byte { return cffBuildCIDCharstrings(5, 600) },
		"cff.cidcs_-1_600": func() []byte { return cffBuildCIDCharstrings(-1, 600) },
		"cff.cff2":         cffBuildCFF2,
		"cff.index": func() []byte {
			return helpersCat(cffIndex(nil), cffIndex([][]byte{[]byte(cffFontName)}),
				cffIndex([][]byte{bytes.Repeat([]byte("x"), 300), []byte("y")}), cffIndex([][]byte{{}, []byte("ab")}))
		},
		"cff.cff2index": func() []byte {
			return helpersCat(cffCFF2Index(nil), cffCFF2Index([][]byte{[]byte("abc"), []byte("de")}))
		},
		"cff.dictint": func() []byte {
			return helpersCat(cffDictInt(0), cffDictInt(-5), cffDictInt(123456789), cffDictInt(-1<<31))
		},
		"cff.t2num": func() []byte {
			var out []byte
			for _, v := range []int{-1131, -1000, -108, -107, -1, 0, 1, 107, 108, 400, 1131} {
				out = append(out, cffT2Num(v)...)
			}
			return out
		},
		"cff.rect": func() []byte {
			return helpersCat(cffRectCharstring(400), cffRectCharstring(500), cffRectCharstring(1000))
		},
		"cff.cff2rect": func() []byte { return helpersCat(cffCFF2Rect(100), cffCFF2Rect(-700)) },
		"cff.u3": func() []byte {
			return helpersCat(cffBuild()[:4], cffIndex([][]byte{[]byte(cffFontName)}), cffIndex(nil), cffIndex(nil), cffIndex(nil))
		},
		"t1.default": func() []byte { return helpersT1(t1Build(t1Options{})) },
		"t1.ok": func() []byte {
			return helpersT1(t1Build(t1Options{Name: "OK", Encoding: t1EncodingArray(ab, true)}))
		},
		"t1.nocs": func() []byte {
			return helpersT1(t1Build(t1Options{Name: "NC", Encoding: t1EncodingArray(ab, true), OmitCharStrings: true}))
		},
		"t1.qshyphen": func() []byte {
			g := append(letters("left right outer inner deep nest software plain text"), "sfthyphen")
			return helpersT1(t1Build(t1Options{Name: "LithoSoftHyphen", Glyphs: g}))
		},
		"t1.texlig": func() []byte {
			g := append(letters("veri\x0ccation text", "code space"), "T0C")
			return helpersT1(t1Build(t1Options{Name: "LithoTeX", Glyphs: g}))
		},
		"t1.pair": func() []byte {
			// sorted(set(b"Hello world")), the space named "space".
			seen := map[byte]bool{}
			var codes []int
			for _, c := range []byte("Hello world") {
				if !seen[c] {
					seen[c] = true
					codes = append(codes, int(c))
				}
			}
			sort.Ints(codes)
			var g []string
			for _, c := range codes {
				if c == ' ' {
					g = append(g, "space")
				} else {
					g = append(g, string(rune(c)))
				}
			}
			return helpersT1(t1Build(t1Options{Name: "LithoPair", Glyphs: g}))
		},
		"t1.n1": func() []byte {
			return helpersT1(t1Build(t1Options{Name: "N1", Encoding: t1EncodingArray(ab, true)}))
		},
		"t1.n2": func() []byte {
			return helpersT1(t1Build(t1Options{Name: "N2", Encoding: t1EncodingArray(ab, false)}))
		},
		"t1.noglyphs":   func() []byte { return helpersT1(t1Build(t1Options{Glyphs: []string{}})) },
		"t1.enc_fill":   func() []byte { return t1EncodingArray(map[int]string{66: "B", 65: "A", 32: "space"}, true) },
		"t1.enc_nofill": func() []byte { return t1EncodingArray(map[int]string{200: "Eacute", 65: "A"}, false) },
		"t1.enc_empty":  func() []byte { return t1EncodingArray(map[int]string{}, true) },
		"t1.encrypt": func() []byte {
			all := make([]byte, 256)
			for i := range all {
				all[i] = byte(i)
			}
			return helpersCat(t1Encrypt([]byte("\x00\x00\x00\x00hello world"), t1EexecKey), t1Encrypt(all, t1CharstringKey))
		},
		"t1.num": func() []byte {
			var out []byte
			for _, v := range []int{-1131, -500, -108, -107, 0, 107, 108, 600, 1131} {
				out = append(out, t1Num(v)...)
			}
			return out
		},
		"t1.charstring": func() []byte { return helpersCat(t1Charstring(0), t1Charstring(500), t1Charstring(1100)) },
		"icc.rgb":       iccRGBDisplayProfile,
		"icc.cmyk":      iccCMYKPrinterProfile,
		"icc.s15": func() []byte {
			return iccS15(0.9642, 1.0, 0.8249, -0.5, 0.4361*0.9, 1.0/131072, 3.0/131072, -1.0/131072, 0)
		},
		"icc.s15_identity": func() []byte { return iccS15(iccIdentity...) },
		"icc.desc": func() []byte {
			return helpersCat(iccTextDescription("litho RGB display"), iccTextDescription(""))
		},
		"icc.xyz": func() []byte { return helpersCat(iccXYZ(iccD50...), iccXYZ(0.1, 0.2, 0.3)) },
		"icc.curves": func() []byte {
			return helpersCat(iccGammaCurve(2.2), iccGammaCurve(1.0), iccGammaCurve(1.8), iccGammaCurve(2.5/256))
		},
		"icc.lut8_grey": func() []byte { return iccLut8(1, 3, []int{0, 128, 128, 255, 128, 128}) },
		"icc.lut8_33": func() []byte {
			clut := make([]int, 24)
			for i := range clut {
				clut[i] = i * 9 % 256
			}
			return iccLut8(3, 3, clut)
		},
		"icc.a2b0": iccA2B0,
		"icc.b2a0": iccB2A0,
		"icc.common": func() []byte {
			var out []byte
			for _, tag := range iccCommonTags {
				out = append(append(out, tag.Sig...), tag.Data...)
			}
			return out
		},
		"icc.grey": func() []byte {
			tags := append([]iccTag{{"desc", iccTextDescription("litho grey display")}}, iccCommonTags...)
			tags = append(tags, iccTag{"kTRC", iccGammaCurve(2.2)})
			return iccProfile("mntr", "GRAY", "XYZ ", tags)
		},
		"icc.tintwhite": func() []byte {
			tags := append([]iccTag{{"desc", iccTextDescription("litho tinted white RGB")}}, iccCommonTags...)
			for _, p := range iccPrimaries {
				v := p.XYZ
				if p.Sig == "bXYZ" {
					v = []float64{0.9 * v[0], 0.9 * v[1], 0.9 * v[2]}
				}
				tags = append(tags, iccTag{p.Sig, iccXYZ(v...)})
			}
			for _, sig := range []string{"rTRC", "gTRC", "bTRC"} {
				tags = append(tags, iccTag{sig, iccGammaCurve(2.2)})
			}
			return iccProfile("mntr", "RGB ", "XYZ ", tags)
		},
		"icc.empty": func() []byte { return iccProfile("scnr", "RGB ", "Lab ", nil) },
		"icc.oddtag": func() []byte {
			return iccProfile("prtr", "CMYK", "Lab ", []iccTag{{"zzzz", []byte("abc")}, {"yyyy", []byte{}}})
		},
		"jp2.two":   func() []byte { return jp2Two },
		"jp2.three": func() []byte { return jp2Three },
		"jp2.ok":    func() []byte { return jp2Container(jp2Three, 3, 8, [][]byte{jp2Colr(1, 1, srgb)}, nil) },
		"jp2.chan":  func() []byte { return jp2Container(jp2Two, 2, 8, [][]byte{jp2Colr(1, 1, srgb)}, nil) },
		"jp2.specs": func() []byte {
			return jp2Container(jp2Three, 3, 8, [][]byte{jp2Colr(1, 1, srgb), jp2Colr(1, 1, srgb)}, nil)
		},
		"jp2.meth": func() []byte { return jp2Container(jp2Three, 3, 8, [][]byte{jp2Colr(4, 1, 0)}, nil) },
		"jp2.jab":  func() []byte { return jp2Container(jp2Three, 3, 8, [][]byte{jp2Colr(1, 1, jp2EnumCIEJab)}, nil) },
		"jp2.depth": func() []byte {
			return jp2Container(jp2Three, 3, 8, [][]byte{jp2Colr(1, 1, srgb)}, []int{40, 40, 40})
		},
		"jp2.depth_empty": func() []byte { return jp2Container(jp2Three, 3, 8, nil, []int{}) },
		"jp2.romm":        func() []byte { return jp2Container(jp2Three, 3, 8, [][]byte{jp2Colr(1, 1, 21)}, nil) },
		"jp2.esrgb":       func() []byte { return jp2Container(jp2Three, 3, 8, [][]byte{jp2Colr(1, 1, 20)}, nil) },
		"jp2.icc": func() []byte {
			return jp2Container([]byte{0, 1}, 1, 8, [][]byte{jp2Box("colr", append([]byte{3, 0, 1}, "profile"...))}, nil)
		},
		"jp2.boxes": func() []byte {
			return helpersCat(jp2Box("cdef", []byte{0, 1}), jp2Ihdr(4, 8), jp2Colr(1, 0, srgb), jp2Colr(2, 0, 0), jp2Bpcc([]int{1, 8, 16}))
		},
	}
	return cases
}
