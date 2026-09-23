package mq

import (
	"bytes"
	"testing"
)

// t88TestDecisions and t88TestCoded are the test sequence of ITU-T T.88,
// Annex H.2: 256 decisions packed most significant bit first, and the 30 bytes
// the encoder produces for them in a single context starting at index 0 with
// MPS 0.
var (
	t88TestDecisions = []byte{
		0x00, 0x02, 0x00, 0x51, 0x00, 0x00, 0x00, 0xC0, 0x03, 0x52, 0x87, 0x2A, 0xAA, 0xAA, 0xAA, 0xAA,
		0x82, 0xC0, 0x20, 0x00, 0xFC, 0xD7, 0x9E, 0xF6, 0xBF, 0x7F, 0xED, 0x90, 0x4F, 0x46, 0xA3, 0xBF,
	}
	t88TestCoded = []byte{
		0x84, 0xC7, 0x3B, 0xFC, 0xE1, 0xA1, 0x43, 0x04, 0x02, 0x20, 0x00, 0x00, 0x41, 0x0D, 0xBB, 0x86,
		0xF4, 0x31, 0x7F, 0xFF, 0x88, 0xFF, 0x37, 0x47, 0x1A, 0xDB, 0x6A, 0xDF, 0xFF, 0xAC,
	}
)

// decodeBytes decodes n bytes' worth of decisions in one context and packs
// them most significant bit first.
func decodeBytes(data []byte, n int) []byte {
	d := NewDecoder(data)
	var cx Context
	out := make([]byte, n)
	for i := range n * 8 {
		out[i/8] |= byte(d.Decode(&cx)) << (7 - i%8)
	}
	return out
}

// TestDecoder_T88AnnexH2 pins the decoder against the only normative-quality
// vector the standard gives. The sequence passes through stuffed bytes (0xFF
// 0x88, 0xFF 0x37), both conditional exchanges and the SWITCH states, and ends
// on the 0xFF 0xAC marker, so it exercises every procedure in Annex E.3.
func TestDecoder_T88AnnexH2(t *testing.T) {
	t.Parallel()

	got := decodeBytes(t88TestCoded, len(t88TestDecisions))
	if !bytes.Equal(got, t88TestDecisions) {
		t.Errorf("decoded\n  % X\nwant\n  % X", got, t88TestDecisions)
	}
}

// TestDecoder_PastTheEndReadsAsMarkerOnes pins what a truncated segment
// decodes to. Past the end the decoder must behave exactly as if the data
// were followed by 0xFF bytes, which BYTEIN reads as a marker and answers with
// 1-bits, rather than reading out of bounds or stopping.
func TestDecoder_PastTheEndReadsAsMarkerOnes(t *testing.T) {
	t.Parallel()

	for _, cut := range []int{0, 1, 2, 5, 13, 20, 29} {
		truncated := t88TestCoded[:cut]
		padded := append(append([]byte(nil), truncated...), 0xFF, 0xFF, 0xFF, 0xFF)
		got := decodeBytes(truncated, 64)
		want := decodeBytes(padded, 64)
		if !bytes.Equal(got, want) {
			t.Errorf("cut at %d: truncated data decodes to\n  % X\nbut the same data followed by 0xFF decodes to\n  % X",
				cut, got, want)
		}
	}
}

// TestDecoder_ContextsAdaptIndependently pins that a decision adapts only the
// context it was coded in: decoding the H.2 sequence while a second, unused
// context sits beside it must leave that context in its reset state.
func TestDecoder_ContextsAdaptIndependently(t *testing.T) {
	t.Parallel()

	d := NewDecoder(t88TestCoded)
	var used, idle Context
	for range 256 {
		d.Decode(&used)
	}
	if idle != (Context{}) {
		t.Errorf("an unused context changed state to %+v", idle)
	}
	if used == (Context{}) {
		t.Error("the context 256 decisions were coded in never left its reset state")
	}
}

// TestQeTable_TransitionsStayInTheTable pins the property the decoder relies
// on to index the table without a bounds check: every NMPS and NLPS entry
// names a row of the table.
func TestQeTable_TransitionsStayInTheTable(t *testing.T) {
	t.Parallel()

	for i, s := range qeTable {
		if int(s.nmps) >= len(qeTable) || int(s.nlps) >= len(qeTable) {
			t.Errorf("row %d transitions to %d/%d, outside the %d-row table", i, s.nmps, s.nlps, len(qeTable))
		}
		if s.qe == 0 {
			t.Errorf("row %d has Qe 0, which would stall renormalisation", i)
		}
	}
}
