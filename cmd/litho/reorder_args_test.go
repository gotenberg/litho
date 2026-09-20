package main

import (
	"bytes"
	"context"
	"flag"
	"os"
	"reflect"
	"strings"
	"testing"
)

// reorderFixture builds a FlagSet shaped like the read commands: one value
// flag and one boolean, which is what reorderArgs inspects through VisitAll.
func reorderFixture() *flag.FlagSet {
	fs := flag.NewFlagSet("fixture", flag.ContinueOnError)
	fs.SetOutput(nil)
	fs.String("f", "text", "format")
	fs.String("p", "", "password")
	fs.Bool("xmp", false, "raw packet")
	return fs
}

// TestReorderArgs_MovesFlagsAheadOfPositionals pins the helper every command in
// this package routes its arguments through: 36 call sites, and it had no test.
//
// The bare "-" case is the defect this file was written for. reorderArgs asks
// strings.HasPrefix(arg, "-"), which is true of "-" itself, so the stdin marker
// was filed under FLAGS rather than positionals. Go's flag.Parse treats a lone
// "-" as a non-flag terminator, so everything after it stopped being parsed:
// `litho metadata - -f json` tried to open "-f" as a PDF. The one positional
// that looks like a flag defeated the helper whose entire purpose is letting
// flags follow positionals.
//
// "-" is not an exotic input here: openPDF documents it as "read from stdin"
// and createOutput as "write to stdout".
func TestReorderArgs_MovesFlagsAheadOfPositionals(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{
			"flags already first",
			[]string{"-f", "json", "doc.pdf"},
			[]string{"-f", "json", "doc.pdf"},
		},
		{
			"flags after a file",
			[]string{"doc.pdf", "-f", "json"},
			[]string{"-f", "json", "doc.pdf"},
		},
		{
			"boolean flag consumes nothing",
			[]string{"doc.pdf", "-xmp"},
			[]string{"-xmp", "doc.pdf"},
		},
		{
			"equals form carries its own value",
			[]string{"doc.pdf", "-f=json"},
			[]string{"-f=json", "doc.pdf"},
		},
		{
			"bare dash is a positional, not a flag",
			[]string{"-", "-f", "json"},
			[]string{"-f", "json", "-"},
		},
		{
			"bare dash as the value of a flag stays its value",
			[]string{"-p", "-", "doc.pdf"},
			[]string{"-p", "-", "doc.pdf"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := reorderArgs(reorderFixture(), tc.args)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("reorderArgs(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// TestMetadata_AcceptsFlagsAfterTheStdinMarker proves the unit defect above
// reaches the user, through the command a caller would actually run.
//
// It swaps os.Stdin, which openPDF reads directly, so it must not run in
// parallel with anything else in this package.
func TestMetadata_AcceptsFlagsAfterTheStdinMarker(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	f, err := os.Open(src) //nolint:gosec // a fixture this test just wrote
	if err != nil {
		t.Fatalf("opening the fixture for stdin: %v", err)
	}
	defer f.Close() //nolint:errcheck // read-only fixture handle

	saved := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = saved })

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"metadata", "-", "-f", "json"}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("metadata - -f json: exit %d, stderr: %s", rc, stderr.String())
	}
	if !strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") {
		t.Errorf("stdout = %q, want JSON: the -f flag after the stdin marker was not parsed",
			strings.TrimSpace(stdout.String()))
	}
}

// TestMetadata_AcceptsFlagsAfterAFilePositional is the control that makes the
// test above attributable.
//
// Reordering works for an ordinary file, so a failure there is specific to the
// bare "-" rather than reorderArgs being broken outright.
func TestMetadata_AcceptsFlagsAfterAFilePositional(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"metadata", src, "-f", "json"}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("metadata <file> -f json: exit %d, stderr: %s", rc, stderr.String())
	}
	if !strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") {
		t.Errorf("stdout = %q, want JSON", strings.TrimSpace(stdout.String()))
	}
}
