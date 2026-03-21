package jbig2

import "testing"

func TestReader_ReadBits(t *testing.T) {
	// 0b10110001 = 0xB1
	data := []byte{0xB1}
	r := NewReader(data)

	// Read 4 bits: should be 1011 = 11.
	v, err := r.ReadBits(4)
	if err != nil {
		t.Fatal(err)
	}
	if v != 11 {
		t.Errorf("ReadBits(4) = %d, want 11", v)
	}

	// Read 4 more bits: 0001 = 1.
	v, err = r.ReadBits(4)
	if err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Errorf("ReadBits(4) = %d, want 1", v)
	}
}

func TestReader_ReadBit(t *testing.T) {
	data := []byte{0xA5} // 10100101
	r := NewReader(data)

	expected := []int{1, 0, 1, 0, 0, 1, 0, 1}
	for i, want := range expected {
		bit, err := r.ReadBit()
		if err != nil {
			t.Fatalf("bit %d: %v", i, err)
		}
		if bit != want {
			t.Errorf("bit %d: got %d, want %d", i, bit, want)
		}
	}
}

func TestReader_Seek(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03}
	r := NewReader(data)

	r.SeekTo(2)
	b, err := r.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	if b != 0x03 {
		t.Errorf("after seek(2): got 0x%02x, want 0x03", b)
	}
}

func TestSubReader(t *testing.T) {
	data := []byte{0x10, 0x20, 0x30, 0x40, 0x50}
	sr := SubReader(data, 1, 3)

	if sr.Length() != 3 {
		t.Fatalf("SubReader length = %d, want 3", sr.Length())
	}

	b, err := sr.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	if b != 0x20 {
		t.Errorf("first byte = 0x%02x, want 0x20", b)
	}
}
