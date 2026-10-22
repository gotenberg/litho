package panicerr

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorWrapsTheSentinel(t *testing.T) {
	var err error = &Error{Where: "worker", Value: "boom", Stack: []byte("stack")}
	wrapped := fmt.Errorf("decoding: %w", err)
	if !errors.Is(wrapped, ErrInternalPanic) {
		t.Fatalf("errors.Is(%v, ErrInternalPanic) = false", wrapped)
	}
	var pe *Error
	if !errors.As(wrapped, &pe) || string(pe.Stack) != "stack" {
		t.Fatalf("errors.As lost the recovered stack: %v", wrapped)
	}
	if msg := err.Error(); !strings.Contains(msg, "worker") || !strings.Contains(msg, "boom") {
		t.Errorf("message %q names neither the worker nor the value", msg)
	}
}
