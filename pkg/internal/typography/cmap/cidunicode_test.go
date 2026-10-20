package cmap

import (
	"context"
	"testing"
)

// TestWithCIDValuesAsUnicode pins that a /ToUnicode program written as an
// encoding CMap, its destinations the Unicode values of the codes, maps each
// code to that value, keeps the bfchar entry a code already has, and leaves
// the CID entries out of the strict view.
func TestWithCIDValuesAsUnicode(t *testing.T) {
	t.Parallel()

	data := []byte("begincmap\n" +
		"1 begincodespacerange\n<0003> <0CE6>\nendcodespacerange\n" +
		"1 beginbfchar\n<002B> <0049>\nendbfchar\n" +
		"2 begincidrange\n<0003> <0003> 32\n<002B> <002C> 72\nendcidrange\n" +
		"1 begincidchar\n<0010> 0\nendcidchar\nendcmap\n")
	c, err := Parse(context.Background(), data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err := WithCIDValuesAsUnicode(context.Background(), c, data)
	if err != nil {
		t.Fatalf("WithCIDValuesAsUnicode: %v", err)
	}
	for code, want := range map[string]string{
		"\x00\x03": " ",
		"\x00\x2B": "I", // the bfchar entry stands
		"\x00\x2C": "I",
	} {
		if v := got.Mapping[code]; v != want {
			t.Errorf("Mapping[%x] = %q, want %q", code, v, want)
		}
	}
	if _, ok := got.Mapping["\x00\x10"]; ok {
		t.Error("a CID entry giving 0 mapped its code")
	}
	if _, ok := got.StrictMapping()["\x00\x03"]; ok {
		t.Error("the strict view holds a CID entry")
	}
	if n, s, ok := got.Map([]byte{0x00, 0x03}); !ok || n != 2 || s != " " {
		t.Errorf("Map(<0003>) = %d, %q, %v, want 2, \" \", true", n, s, ok)
	}
}

// TestWithCIDValuesAsUnicode_IdentityProgram pins that the Identity-H
// program written as a /ToUnicode, every two-byte code to itself, maps
// nothing: it says nothing of the characters, and PDF.js reads it as no
// /ToUnicode (pdfjs issue5801.pdf).
func TestWithCIDValuesAsUnicode_IdentityProgram(t *testing.T) {
	t.Parallel()

	data := []byte("begincmap\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
		"2 begincidrange\n<0000> <7fff> 0\n<8000> <ffff> 32768\nendcidrange\nendcmap\n")
	c, err := Parse(context.Background(), data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err := WithCIDValuesAsUnicode(context.Background(), c, data)
	if err != nil {
		t.Fatalf("WithCIDValuesAsUnicode: %v", err)
	}
	if len(got.Mapping) != 0 {
		t.Fatalf("the Identity-H program mapped %d codes, want none", len(got.Mapping))
	}
}
