package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// litho declares exitTimeout (124), the conventional status `timeout(1)` uses,
// and advertises --timeout in its own usage. Nothing returned it: a deadline
// that expired surfaced as exitError, indistinguishable from a corrupt file or
// a bad flag, so a caller that set a deadline could not tell "took too long"
// from "could not do it".
//
// The conversion is deliberately narrow. Only a FAILURE becomes a timeout: a
// run that produced a real verdict before the deadline fired keeps it, because
// `--timeout 1ms validate` on a non-conformant file was measured returning 2,
// having reported its violations before the deadline expired. Converting that
// to 124 would destroy a genuine answer.

// expiredDeadline is already in the past when context.WithTimeout builds it, so
// the first ctx.Err() check fails. That makes these tests deterministic rather
// than a race against the work finishing.
const expiredDeadline = "1ns"

// TestRun_ExpiredDeadlineReportsTheTimeoutStatus is the defect. The control
// carries it: the same command without a deadline must NOT return 124, or the
// assertion would pass on a command that fails that way anyway.
func TestRun_ExpiredDeadlineReportsTheTimeoutStatus(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"info", path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("control: plain `info` exits %d, want %d; this fixture cannot test a deadline: %s",
			rc, exitOK, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	rc := run(context.Background(),
		[]string{"--timeout", expiredDeadline, "info", path}, &stdout, &stderr)
	if rc != exitTimeout {
		t.Errorf("an expired deadline exits %d, want %d: a caller cannot tell a timeout "+
			"from any other failure; stderr: %s", rc, exitTimeout, stderr.String())
	}
}

// TestRun_TimeoutStatusReachesEveryCommand: the mapping sits at the single
// dispatch point, so it must hold for a read command and a write command
// alike, not just the one that exposed it.
func TestRun_TimeoutStatusReachesEveryCommand(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())

	for _, args := range [][]string{
		{"info", path},
		{"pages", path},
		{"validate", "--profile", "PDF/A-2b", path},
	} {
		full := append([]string{"--timeout", expiredDeadline}, args...)
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), full, &stdout, &stderr); rc != exitTimeout {
			t.Errorf("%v with an expired deadline exits %d, want %d; stderr: %s",
				args, rc, exitTimeout, stderr.String())
		}
	}
}

// TestRun_GenerousTimeoutKeepsTheRealVerdict is the control that stops the
// conversion swallowing answers. validate reports a non-conformant document as
// exitValidationFail, and a deadline that never fires must not change that.
func TestRun_GenerousTimeoutKeepsTheRealVerdict(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"--timeout", "60s", "validate", "--profile", "PDF/A-2b", path}, &stdout, &stderr)
	if rc == exitTimeout {
		t.Fatalf("a deadline that never fired reported a timeout; stderr: %s", stderr.String())
	}
	if rc != exitValidationFail && rc != exitOK {
		t.Errorf("validate under a generous deadline exits %d, want %d or %d; stderr: %s",
			rc, exitValidationFail, exitOK, stderr.String())
	}
}

// TestRun_NoDeadlineNeverReportsTimeout: without --timeout there is no
// deadline to expire, so a failing command keeps exitError.
func TestRun_NoDeadlineNeverReportsTimeout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"info", "/nonexistent/file.pdf"}, &stdout, &stderr)
	if rc == exitTimeout {
		t.Errorf("a failure with no deadline reported %d; stderr: %s", exitTimeout, stderr.String())
	}
	if rc != exitError {
		t.Errorf("a missing file exits %d, want %d", rc, exitError)
	}
}

// TestRun_TimeoutStatusIsTheConventionalOne pins 124 rather than an arbitrary
// value: it is what timeout(1) reports, which is why a CI caller recognises it.
func TestRun_TimeoutStatusIsTheConventionalOne(t *testing.T) {
	t.Parallel()
	if exitTimeout != 124 {
		t.Errorf("exitTimeout = %d, want 124, the status timeout(1) uses", exitTimeout)
	}
}

// TestRun_UsageStillDocumentsTheTimeoutFlag: the status is only discoverable
// if the flag is.
func TestRun_UsageStillDocumentsTheTimeoutFlag(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	printUsage(&buf)
	if !strings.Contains(buf.String(), "--timeout") {
		t.Errorf("usage no longer documents --timeout:\n%s", buf.String())
	}
}

// TestTimeoutStatus is where the NARROWNESS is pinned, and it exists because
// the end-to-end tests above cannot pin it.
//
// An already-expired deadline makes every command fail before it produces a
// verdict, so driving `run` can only ever show "failed with a dead context",
// which looks identical whether the rule checks the status or ignores it.
// Proven, not assumed: removing the exitError guard left all six end-to-end
// tests passing. The rows that matter are the non-error statuses, because they
// are what a blanket conversion would destroy.
func TestTimeoutStatus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		code   int
		ctxErr error
		want   int
	}{
		// The repair: a failure the deadline caused is reported as a timeout.
		{"failure with an expired deadline becomes a timeout", exitError, context.DeadlineExceeded, exitTimeout},

		// The narrowness. A command that reached a real answer keeps it, even
		// though the deadline has since expired.
		{"a non-conformant verdict survives", exitValidationFail, context.DeadlineExceeded, exitValidationFail},
		{"a success survives", exitOK, context.DeadlineExceeded, exitOK},

		// No deadline, nothing to convert.
		{"failure with no context error", exitError, nil, exitError},
		{"success with no context error", exitOK, nil, exitOK},

		// Cancellation is not a timeout: this command installs none, and a
		// caller that cancels asked for a stop, not a deadline.
		{"cancellation is not a timeout", exitError, context.Canceled, exitError},

		// A wrapped deadline still counts: errors.Is, not equality.
		{
			"a wrapped deadline still converts", exitError,
			fmt.Errorf("parsing xref: %w", context.DeadlineExceeded), exitTimeout,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := timeoutStatus(tc.code, tc.ctxErr); got != tc.want {
				t.Errorf("timeoutStatus(%d, %v) = %d, want %d", tc.code, tc.ctxErr, got, tc.want)
			}
		})
	}
}

// TestTimeoutStatus_UsesErrorsIs guards the one implementation choice a
// refactor is most likely to undo: litho wraps context errors as it propagates
// them, so an equality check would silently stop converting.
func TestTimeoutStatus_UsesErrorsIs(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("resolving catalog: %w", context.DeadlineExceeded)
	if !errors.Is(wrapped, context.DeadlineExceeded) {
		t.Fatal("the fixture is not a wrapped deadline; this test proves nothing")
	}
	if got := timeoutStatus(exitError, wrapped); got != exitTimeout {
		t.Errorf("a wrapped deadline gave %d, want %d: the check is comparing by equality", got, exitTimeout)
	}
}
