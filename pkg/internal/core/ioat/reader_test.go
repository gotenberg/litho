package ioat

import (
	"bytes"
	"io"
	"testing"
)

func TestNewReader_ReadByte(t *testing.T) {
	data := []byte("ABCDE")
	r := NewReader(bytes.NewReader(data), int64(len(data)), 0)

	for i, want := range data {
		got, err := r.ReadByte()
		if err != nil {
			t.Fatalf("ReadByte() at index %d: %v", i, err)
		}
		if got != want {
			t.Errorf("ReadByte() at index %d = %c, want %c", i, got, want)
		}
	}

	_, err := r.ReadByte()
	if err != io.EOF {
		t.Errorf("ReadByte() past end: got %v, want io.EOF", err)
	}
}

func TestUnreadByte(t *testing.T) {
	data := []byte("XY")
	r := NewReader(bytes.NewReader(data), int64(len(data)), 0)

	b, _ := r.ReadByte()
	if b != 'X' {
		t.Fatalf("first byte = %c, want X", b)
	}

	if err := r.UnreadByte(); err != nil {
		t.Fatalf("UnreadByte: %v", err)
	}

	b, _ = r.ReadByte()
	if b != 'X' {
		t.Errorf("re-read after UnreadByte = %c, want X", b)
	}
}

func TestUnreadByte_AtStart(t *testing.T) {
	r := NewReader(bytes.NewReader([]byte("A")), 1, 0)
	err := r.UnreadByte()
	if err == nil {
		t.Error("UnreadByte at start should return error")
	}
}

func TestRead(t *testing.T) {
	data := []byte("Hello, World!")
	r := NewReader(bytes.NewReader(data), int64(len(data)), 0)

	buf := make([]byte, 5)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 || string(buf) != "Hello" {
		t.Errorf("Read = %q (%d bytes), want %q", buf[:n], n, "Hello")
	}
}

func TestRead_EmptySlice(t *testing.T) {
	r := NewReader(bytes.NewReader([]byte("A")), 1, 0)
	n, err := r.Read([]byte{})
	if n != 0 || err != nil {
		t.Errorf("Read(empty) = %d, %v; want 0, nil", n, err)
	}
}

func TestSeek_Start(t *testing.T) {
	data := []byte("ABCDEFGH")
	r := NewReader(bytes.NewReader(data), int64(len(data)), 0)

	pos, err := r.Seek(3, io.SeekStart)
	if err != nil {
		t.Fatal(err)
	}
	if pos != 3 {
		t.Errorf("Seek returned pos=%d, want 3", pos)
	}

	b, _ := r.ReadByte()
	if b != 'D' {
		t.Errorf("ReadByte after seek = %c, want D", b)
	}
}

func TestSeek_End(t *testing.T) {
	data := []byte("ABCDEFGH")
	r := NewReader(bytes.NewReader(data), int64(len(data)), 0)

	pos, err := r.Seek(-2, io.SeekEnd)
	if err != nil {
		t.Fatal(err)
	}
	if pos != 6 {
		t.Errorf("Seek from end returned pos=%d, want 6", pos)
	}

	b, _ := r.ReadByte()
	if b != 'G' {
		t.Errorf("ReadByte after seek end = %c, want G", b)
	}
}

func TestSeek_Current(t *testing.T) {
	data := []byte("ABCDEFGH")
	r := NewReader(bytes.NewReader(data), int64(len(data)), 0)

	// Read 2 bytes first
	_, _ = r.ReadByte()
	_, _ = r.ReadByte()

	pos, err := r.Seek(1, io.SeekCurrent)
	if err != nil {
		t.Fatal(err)
	}
	if pos != 3 {
		t.Errorf("Seek current returned pos=%d, want 3", pos)
	}

	b, _ := r.ReadByte()
	if b != 'D' {
		t.Errorf("ReadByte after seek current = %c, want D", b)
	}
}

func TestSeek_NegativePosition(t *testing.T) {
	r := NewReader(bytes.NewReader([]byte("AB")), 2, 0)
	_, err := r.Seek(-1, io.SeekStart)
	if err == nil {
		t.Error("Seek to negative position should return error")
	}
}

func TestSeek_InvalidWhence(t *testing.T) {
	r := NewReader(bytes.NewReader([]byte("AB")), 2, 0)
	_, err := r.Seek(0, 99)
	if err == nil {
		t.Error("Seek with invalid whence should return error")
	}
}

func TestNewReader_WithOffset(t *testing.T) {
	data := []byte("ABCDEFGH")
	r := NewReader(bytes.NewReader(data), int64(len(data)), 4)

	b, err := r.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	if b != 'E' {
		t.Errorf("ReadByte with offset 4 = %c, want E", b)
	}
}
