//go:build cgo

package jbig2

import (
	"bytes"
	"testing"
)

// TestDecoderRejectsMalformedInput pins that the libjbig2dec path reports an
// error on input that is not a JBIG2 stream, rather than panicking or
// returning pixels.
//
// The CGO decoder had no test of its own: decoder_nocgo_test.go is itself
// //go:build !cgo, so enabling CGO removed the only test and added none,
// leaving the whole C interop uncovered. That is the code where a mistake
// costs a crash rather than a wrong answer.
//
// There is no positive counterpart here as there is for JPX: no JBIG2 encoder
// is available to generate a fixture, and lifting a stream out of the
// third-party corpora to embed in the repository is not appropriate. Decoding
// a real stream stays covered by the corpus runs.
func TestDecoderRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"random bytes", []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x11, 0x22, 0x33}},
		{"text", []byte("this is not a JBIG2 segment stream")},
		{"zeros", make([]byte, 512)},
		{"one byte", []byte{0x00}},
		{"truncated segment header", []byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x01}},
	} {
		out, err := New().Decode(tc.data, nil)
		if err == nil {
			t.Errorf("%s: decoded %d bytes from input that is not JBIG2", tc.name, len(out))
		}
		if out != nil {
			t.Errorf("%s: returned %d bytes alongside an error", tc.name, len(out))
		}
	}
}

// TestDecoderRejectsMalformedGlobals pins the /JBIG2Globals path, which is
// specific to this filter: the globals bytes are handed to the C library as a
// second stream, so malformed globals must fail the same way malformed page
// data does (ISO 32000-1, 7.4.7).
func TestDecoderRejectsMalformedGlobals(t *testing.T) {
	t.Parallel()

	data := []byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x02, 0x03}
	parms := map[string]any{"JBIG2Globals": []byte{0xFF, 0xFE, 0xFD, 0xFC}}
	if out, err := New().Decode(data, parms); err == nil {
		t.Errorf("decoded %d bytes from malformed data and globals", len(out))
	}
}

// TestDecoderIgnoresAGlobalsValueOfTheWrongType pins that a /JBIG2Globals
// entry that is not a byte slice is skipped rather than asserted on: the parms
// map comes from the document, so its values are untrusted.
func TestDecoderIgnoresAGlobalsValueOfTheWrongType(t *testing.T) {
	t.Parallel()

	data := []byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x02, 0x03}
	for _, bad := range []any{"a string", 42, nil, []int{1, 2}} {
		if _, err := New().Decode(data, map[string]any{"JBIG2Globals": bad}); err == nil {
			t.Errorf("globals %T: decoded without error", bad)
		}
	}
}

// TestDecoderRejectsEmptyInput pins the guard in the exported wrapper, which is
// the same in both builds.
func TestDecoderRejectsEmptyInput(t *testing.T) {
	t.Parallel()

	if _, err := New().Decode(nil, nil); err == nil {
		t.Error("empty input decoded without error")
	}
}

// TestDecoderSurvivesRepeatedFailures pins that a failed decode releases what
// it allocated: a leak or a double free in the C error path shows up when the
// path is taken often.
func TestDecoderSurvivesRepeatedFailures(t *testing.T) {
	t.Parallel()

	junk := bytes.Repeat([]byte{0x00, 0x01, 0x02, 0x03}, 64)
	d := New()
	for range 200 {
		if _, err := d.Decode(junk, nil); err == nil {
			t.Fatal("junk decoded without error")
		}
	}
}

// TestFilterNameIsTheCanonicalName pins the name the filter registry keys on.
func TestFilterNameIsTheCanonicalName(t *testing.T) {
	t.Parallel()

	if got := New().FilterName(); got != "JBIG2Decode" {
		t.Errorf("FilterName = %q, want %q", got, "JBIG2Decode")
	}
}
