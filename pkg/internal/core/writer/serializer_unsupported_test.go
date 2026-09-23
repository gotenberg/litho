package writer

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// unserializableObject is a PDF object the serializer has no case for.
//
// Every concrete type in the ast package appears in [Serialize]'s type switch,
// so the default branch cannot be reached with a real object and needs a
// stand-in to be exercised at all.
type unserializableObject struct{}

// IsPdfObject marks unserializableObject as a PDF object.
func (unserializableObject) IsPdfObject() {}

// String satisfies the second half of [ast.PdfObject]. The interface demands
// it alongside IsPdfObject, so a stub carrying only the marker does not
// implement it and the test file does not compile.
func (unserializableObject) String() string { return "<unserializable>" }

// TestSerialize_UnsupportedObjectIsMatchable pins that the default branch
// reports [ErrUnsupportedObject] rather than an error only a human can read.
//
// The sentinel documents itself as returned when the serializer meets a type it
// cannot serialize, and it was returned by nothing: the branch built its own
// fmt.Errorf, so [errors.Is] against the documented sentinel was false forever
// and a caller had nothing to match but the message text, which CLAUDE.md
// forbids. The sentinel was the only one in this package and the doc was the
// only place claiming it was ever produced.
func TestSerialize_UnsupportedObjectIsMatchable(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	n, err := Serialize(context.Background(), &buf, unserializableObject{})
	if err == nil {
		t.Fatal("Serialize accepted a type it has no case for")
	}
	if !errors.Is(err, ErrUnsupportedObject) {
		t.Errorf("errors.Is(err, ErrUnsupportedObject) = false, err = %v", err)
	}

	// The wrap must not cost the diagnosis. %T is what tells a caller WHICH
	// type was refused, and a bare sentinel would name none of them, so a
	// wrap that dropped it would trade one unusable error for another.
	if !strings.Contains(err.Error(), "unserializableObject") {
		t.Errorf("the error no longer names the offending type: %v", err)
	}

	// A refused object must leave nothing behind: a partial write would put
	// bytes in the file for an object the serializer just declined.
	if n != 0 {
		t.Errorf("reported %d bytes written for a refused object, want 0", n)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes for a refused object, want 0", buf.Len())
	}
}
