package type1

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// subsetTestProgram builds a Type 1 program of .notdef, space, A to L, Aacute
// (a seac of A and acute, Standard codes 0x41 and 0xC2) and acute.
func subsetTestProgram(subrs, noTrailer bool) ([]byte, [3]int) {
	glyphs := []fontbuild.Glyph{{Name: "space", Advance: 250, Empty: true}}
	for c := 'A'; c <= 'L'; c++ {
		glyphs = append(glyphs, fontbuild.Glyph{Name: string(c), Advance: uint16(600 + c - 'A')})
	}
	glyphs = append(glyphs,
		fontbuild.Glyph{Name: "Aacute", Advance: 600, Seac: &[2]byte{0x41, 0xC2}},
		fontbuild.Glyph{Name: "acute", Advance: 333},
	)
	return fontbuild.Type1{FontName: "TestType1", Glyphs: glyphs, NotdefAdvance: 500, Subrs: subrs, NoTrailer: noTrailer}.Build()
}

// charStrings returns every charstring of a program, decrypted, by name.
func charStrings(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	plain := decryptEexec(eexecRegion(data))
	entries, ok := charStringEntries(plain)
	if !ok {
		t.Fatal("no /CharStrings")
	}
	out := make(map[string][]byte)
	for _, e := range entries {
		out[e.name] = decryptCharString(plain[e.bodyStart:e.bodyEnd], parseLenIV(plain))
	}
	return out
}

// privateBeforeCharStrings returns the decrypted private section up to the
// /CharStrings dictionary: the Private dictionary and its /Subrs.
func privateBeforeCharStrings(data []byte) []byte {
	plain := decryptEexec(eexecRegion(data))
	before, _, _ := bytes.Cut(plain, charStringsMarker)
	return before
}

func TestSubsetInPlace(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name             string
		subrs, noTrailer bool
	}{
		{"plain", false, false},
		{"subroutines", true, false},
		{"no trailer", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src, srcLens := subsetTestProgram(tc.subrs, tc.noTrailer)
			out, lens, err := SubsetInPlace(context.Background(), src, []string{"Aacute", "C", "missing"})
			if err != nil {
				t.Fatalf("SubsetInPlace: %v", err)
			}
			if len(out) >= len(src) {
				t.Errorf("subset of %d bytes, source of %d", len(out), len(src))
			}
			if got := lens.Clear + lens.Encrypted + lens.Trailer; got != len(out) {
				t.Errorf("lengths %+v sum to %d, program of %d bytes", lens, got, len(out))
			}
			if lens.Clear != srcLens[0] || lens.Trailer != srcLens[2] {
				t.Errorf("lengths %+v, source lengths %v: the clear text and trailer must not move", lens, srcLens)
			}
			if !bytes.Equal(out[:lens.Clear], src[:srcLens[0]]) || !bytes.Equal(out[len(out)-lens.Trailer:], src[len(src)-srcLens[2]:]) {
				t.Error("the clear text or the trailer changed")
			}
			if !slices.Equal(ParseCharStringNames(out), ParseCharStringNames(src)) {
				t.Errorf("names %v, want %v", ParseCharStringNames(out), ParseCharStringNames(src))
			}
			if !maps.Equal(ParseCharStringWidths(out), ParseCharStringWidths(src)) {
				t.Errorf("widths %v, want %v", ParseCharStringWidths(out), ParseCharStringWidths(src))
			}
			if ParseEncoding(out) != ParseEncoding(src) {
				t.Error("the encoding changed")
			}
			if !bytes.Equal(privateBeforeCharStrings(out), privateBeforeCharStrings(src)) {
				t.Error("the Private dictionary or its subroutines changed")
			}
			before, after := charStrings(t, src), charStrings(t, out)
			for name, cs := range before {
				switch name {
				case ".notdef", "Aacute", "A", "acute", "C", "space":
					// space and .notdef are already metrics and endchar.
					if !bytes.Equal(after[name], cs) {
						t.Errorf("kept glyph /%s changed", name)
					}
				default:
					want := append(slices.Clip(cs[:4]), csOpEndChar)
					if !bytes.Equal(after[name], want) {
						t.Errorf("glyph /%s = % x, want its metrics and endchar % x", name, after[name], want)
					}
				}
			}
			again, _, err := SubsetInPlace(context.Background(), src, []string{"C", "Aacute"})
			if err != nil || !bytes.Equal(again, out) {
				t.Errorf("a second run differs (err %v)", err)
			}
		})
	}
}

// testTrailerLen is the length of the trailer [fontbuild.Type1] writes:
// eight lines of 64 zeros and cleartomark.
const testTrailerLen = 8*65 + len("cleartomark\n")

// rewritePrivate returns data, a program [subsetTestProgram] built with its
// trailer, with its decrypted private section passed through edit,
// encrypted again.
func rewritePrivate(t *testing.T, data []byte, edit func([]byte) []byte) []byte {
	t.Helper()
	before, _, ok := bytes.Cut(data, eexecMarker)
	if !ok {
		t.Fatal("no eexec")
	}
	start := len(before) + len(eexecMarker) + 1
	end := len(data) - testTrailerLen
	plain := crypt(data[start:end], eexecKey, false)
	out := append(slices.Clip(data[:start]), crypt(edit(plain), eexecKey, true)...)
	return append(out, data[end:]...)
}

// replaceEntry replaces the encrypted bytes of the entry that head opens,
// "/name " or "dup N ", followed by "LEN RD ", with cs encrypted.
func replaceEntry(t *testing.T, plain []byte, head string, cs []byte) []byte {
	t.Helper()
	at := bytes.Index(plain, []byte(head))
	if at < 0 {
		t.Fatalf("no entry %q", head)
	}
	at += len(head)
	n, after := readInt(plain, at)
	if after == at || !bytes.HasPrefix(plain[after:], []byte(" RD ")) {
		t.Fatalf("entry %q has no length", head)
	}
	enc := crypt(append(make([]byte, defaultLenIV), cs...), charStringKey, true)
	out := append(slices.Clip(plain[:at]), strconv.Itoa(len(enc))+" RD "...)
	out = append(out, enc...)
	return append(out, plain[after+len(" RD ")+n:]...)
}

func TestSubsetInPlace_Refuses(t *testing.T) {
	t.Parallel()
	src, _ := subsetTestProgram(true, false)
	plainSrc, _ := subsetTestProgram(false, false)
	hsbw := []byte{139, 139 + 100, 13}
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"pfb segments", append([]byte{0x80, 0x01}, src...)},
		{"no eexec", []byte("%!PS-AdobeFont-1.0: X\n/FontName /X def\n")},
		{"hex eexec", func() []byte {
			before, after, _ := bytes.Cut(src, []byte("currentfile eexec\n"))
			cipher, trailer := after[:len(after)-testTrailerLen], after[len(after)-testTrailerLen:]
			out := append(slices.Clip(before), "currentfile eexec\n"...)
			out = append(out, hexEncode(cipher)...)
			return append(append(out, '\n'), trailer...)
		}()},
		{"unencrypted charstrings", rewritePrivate(t, plainSrc, func(p []byte) []byte {
			return bytes.Replace(p, []byte("/lenIV 4 def"), []byte("/lenIV -1 def"), 1)
		})},
		{"seac in a subroutine", rewritePrivate(t, src, func(p []byte) []byte {
			return replaceEntry(t, p, "dup 0 ", []byte{139, 139, 139, 139 + 65, 139 + 66, 12, 6})
		})},
		{"a /Subrs definition that does not read", rewritePrivate(t, src, func(p []byte) []byte {
			return bytes.Replace(p, []byte(" NP\ndup 1 "), []byte(" XY\ndup 1 "), 1)
		})},
		{"seac over a popped operand", rewritePrivate(t, plainSrc, func(p []byte) []byte {
			cs := append(slices.Clip(hsbw), 139, 139, 139, 139+65, 12, 17, 12, 6)
			return replaceEntry(t, p, "/Aacute ", cs)
		})},
		{"no closefile", rewritePrivate(t, plainSrc, func(p []byte) []byte {
			return bytes.Replace(p, []byte("closefile"), []byte("closefilX"), 1)
		})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := SubsetInPlace(context.Background(), tc.data, []string{"Aacute"}); !errors.Is(err, ErrSubsetUnsupported) {
				t.Errorf("err %v, want ErrSubsetUnsupported", err)
			}
		})
	}
}

// TestSubsetInPlace_SparseSubrs pins that a /Subrs array defining fewer
// entries than it declares, as a subset program's does, still subsets.
func TestSubsetInPlace_SparseSubrs(t *testing.T) {
	t.Parallel()
	src, _ := subsetTestProgram(true, false)
	sparse := rewritePrivate(t, src, func(p []byte) []byte {
		return bytes.Replace(p, []byte("/Subrs 5 array"), []byte("/Subrs 812 array"), 1)
	})
	out, _, err := SubsetInPlace(context.Background(), sparse, []string{"A"})
	if err != nil {
		t.Fatalf("SubsetInPlace: %v", err)
	}
	if len(out) >= len(sparse) {
		t.Errorf("subset of %d bytes, source of %d", len(out), len(sparse))
	}
}

// TestSubsetInPlace_CipherZeroBeforeTrailer pins that a closefile whose
// trailing cipher text byte reads as one of the trailer's zeros is still
// decrypted: the encrypted portion never ends before the white space after
// closefile.
func TestSubsetInPlace_CipherZeroBeforeTrailer(t *testing.T) {
	t.Parallel()
	src, lens := subsetTestProgram(false, false)
	// The salt seeds the whole cipher stream, so some salt makes the line
	// feed after closefile, the last plaintext byte, encrypt to '0'.
	var data []byte
	for salt := range 1 << 16 {
		cand := rewritePrivate(t, src, func(p []byte) []byte {
			p = slices.Clone(p)
			p[0], p[1] = byte(salt>>8), byte(salt)
			return p
		})
		if cand[len(cand)-lens[2]-1] == '0' && cand[lens[0]] > ' ' {
			data = cand
			break
		}
	}
	if data == nil {
		t.Fatal("no salt encrypts the last line feed to '0'")
	}
	out, got, err := SubsetInPlace(context.Background(), data, []string{"A"})
	if err != nil {
		t.Fatalf("SubsetInPlace: %v", err)
	}
	plain := crypt(out[got.Clear:got.Clear+got.Encrypted], eexecKey, false)
	if !bytes.Contains(plain, []byte("closefile\n")) {
		t.Errorf("the white space after closefile is no longer encrypted: % x", plain[len(plain)-12:])
	}
}

func TestSubsetInPlace_Canceled(t *testing.T) {
	t.Parallel()
	src, _ := subsetTestProgram(false, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := SubsetInPlace(ctx, src, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("err %v, want context.Canceled", err)
	}
}
