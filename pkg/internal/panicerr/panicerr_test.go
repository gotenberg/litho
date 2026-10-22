package panicerr

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestErrorWrapsTheSentinel(t *testing.T) {
	t.Parallel()

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

func TestLog_LogsAWorkerPanicOnce(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	err := fmt.Errorf("decoding: %w", &Error{Where: "worker", Value: "boom", Stack: []byte("the stack")})
	for range 3 {
		if got := Log(logger, err); got != err { //nolint:errorlint // Log must return the very error it was given.
			t.Fatalf("Log returned %v, want the error unchanged", got)
		}
	}
	out := buf.String()
	if n := strings.Count(out, "level=ERROR"); n != 1 {
		t.Fatalf("logged %d records, want 1:\n%s", n, out)
	}
	if !strings.Contains(out, "boom") || !strings.Contains(out, "the stack") {
		t.Errorf("the record lacks the value or the stack:\n%s", out)
	}

	buf.Reset()
	plain := errors.New("plain")
	if Log(logger, plain) != plain || Log(logger, nil) != nil || buf.Len() != 0 { //nolint:errorlint // Log must return the very error it was given.
		t.Errorf("an error carrying no panic was logged or changed:\n%s", buf.String())
	}
	if Log(nil, err) != err { //nolint:errorlint // Log must return the very error it was given.
		t.Error("a nil logger changed the error")
	}
}
