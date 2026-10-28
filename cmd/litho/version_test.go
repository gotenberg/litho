package main

import (
	"bytes"
	"context"
	"testing"
)

func TestRun_Version(t *testing.T) {
	saved := version
	t.Cleanup(func() { version = saved })
	version = "v1.2.3"

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"--version"}, &stdout, &stderr)
	if rc != exitOK {
		t.Errorf("exit code: got %d, want %d", rc, exitOK)
	}
	if got, want := stdout.String(), "litho v1.2.3\n"; got != want {
		t.Errorf("stdout: got %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr: got %q, want empty", stderr.String())
	}
}

func TestVersionString_DefaultsToDevel(t *testing.T) {
	saved := version
	t.Cleanup(func() { version = saved })
	version = ""

	// A test binary records no module version, so the fallback applies.
	if got := versionString(); got != "devel" {
		t.Errorf("versionString: got %q, want %q", got, "devel")
	}
}
