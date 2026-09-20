package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// setFormFails runs set-form and asserts it refused: non-zero exit, something
// on stderr, and no output file left behind.
//
// Every failure path in runSetForm reports before createOutput runs, so an
// output file appearing here means one of them moved after it.
func setFormFails(t *testing.T, out string, args []string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), args, &stdout, &stderr); rc == exitOK {
		t.Fatalf("set-form %v succeeded; stderr: %s", args, stderr.String())
	}
	if stderr.Len() == 0 {
		t.Errorf("set-form %v failed silently: nothing on stderr", args)
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("%s exists: a failed set-form must not leave an output file", out)
	}
}

// TestSetForm_WritesTheValueItIsGiven pins the -field wiring: name=value must
// reach the field of that name and leave the others alone.
//
// It reuses formFixturePDF and listFormFields from the flatten-form test
// rather than building a second AcroForm fixture. The fixture declares /T
// (name) with /V (John) and /T (email) with /V (john@example.com), so both the
// change and the non-change are observable in one read.
//
// Reading back through the forms command matters for the same reason it does
// there: litho writes an incremental update, so the superseded widget object
// still carries the old value in the file's bytes. Only the command's view
// reports what a reader sees.
func TestSetForm_WritesTheValueItIsGiven(t *testing.T) {
	src := writeTempPDF(t, formFixturePDF())
	out := filepath.Join(t.TempDir(), "form.pdf")

	before := listFormFields(t, src)
	if len(before) != 2 {
		t.Fatalf("precondition: fixture reports %d field(s), want 2: %+v", len(before), before)
	}

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"set-form", "-field", "name=Jane", "-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("set-form: exit %d, stderr: %s", rc, stderr.String())
	}

	got := map[string]string{}
	for _, f := range listFormFields(t, out) {
		got[f.Name] = f.Value
	}
	if got["name"] != "Jane" {
		t.Errorf("name = %q, want %q: the -field assignment did not reach the widget", got["name"], "Jane")
	}
	if got["email"] != "john@example.com" {
		t.Errorf("email = %q, want it untouched: set-form rewrote a field it was not given", got["email"])
	}
}

// TestSetForm_WritesEveryRepeatedField pins that -field is repeatable, which
// is the whole reason it is a flag.Value rather than a plain string.
func TestSetForm_WritesEveryRepeatedField(t *testing.T) {
	src := writeTempPDF(t, formFixturePDF())
	out := filepath.Join(t.TempDir(), "form.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"set-form", "-field", "name=Jane", "-field", "email=jane@example.com", "-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("set-form: exit %d, stderr: %s", rc, stderr.String())
	}

	got := map[string]string{}
	for _, f := range listFormFields(t, out) {
		got[f.Name] = f.Value
	}
	if got["name"] != "Jane" || got["email"] != "jane@example.com" {
		t.Errorf("fields = %+v, want name=Jane and email=jane@example.com: a repeated -field was dropped", got)
	}
}

// TestSetForm_RejectsAFieldWithoutAnEquals covers the strings.Cut check, which
// is the one failure that happens after the document opens.
//
// A handler that passed the whole token through as a name would satisfy every
// other test here and silently create a junk field.
func TestSetForm_RejectsAFieldWithoutAnEquals(t *testing.T) {
	src := writeTempPDF(t, formFixturePDF())
	out := filepath.Join(t.TempDir(), "form.pdf")

	setFormFails(t, out, []string{"set-form", "-field", "nameJane", "-o", out, src})
}

// TestSetForm_MissingFieldIsAUsageError pins that at least one -field is
// required: without it the command would rewrite the file changing nothing.
func TestSetForm_MissingFieldIsAUsageError(t *testing.T) {
	src := writeTempPDF(t, formFixturePDF())
	out := filepath.Join(t.TempDir(), "form.pdf")

	setFormFails(t, out, []string{"set-form", "-o", out, src})
}

// TestSetForm_MissingOutputIsAUsageError pins the -o requirement.
func TestSetForm_MissingOutputIsAUsageError(t *testing.T) {
	src := writeTempPDF(t, formFixturePDF())
	out := filepath.Join(t.TempDir(), "form.pdf")

	setFormFails(t, out, []string{"set-form", "-field", "name=Jane", src})
}

// TestSetForm_RejectsAnUnreadableInput covers the open path: the named file
// does not exist, so openPDF fails before any field is parsed.
func TestSetForm_RejectsAnUnreadableInput(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "form.pdf")

	setFormFails(t, out, []string{
		"set-form", "-field", "name=Jane", "-o", out, filepath.Join(dir, "absent.pdf"),
	})
}
