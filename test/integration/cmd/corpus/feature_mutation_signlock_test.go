package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// siglockFixture returns the litho corpus fixture whose unsigned signature
// fields carry a /Lock of each action Table 233 defines.
func siglockFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "corpus", "litho", "pdfs", "siglock_2b.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestMutationSign_LockFollowUps runs mutate-sign on the fixture whose
// signature fields lock All, Include and Exclude. Each locked signature is
// followed by a signature in every other field and in a new one, refused
// exactly where the lock reaches, and by a document timestamp that changes no
// field, so the cell matches.
func TestMutationSign_LockFollowUps(t *testing.T) {
	t.Parallel()
	requireQPDF(t)
	f := newMutationFeature(specNamed(t, "sign"))
	v := runMutationFixtures(t, f, siglockFixture(t), "PDF_A-2b")[0]
	if v.Skipped || !v.Match {
		t.Errorf("skipped=%t match=%t reason %q, want a match", v.Skipped, v.Match, v.Reason)
	}
}

// TestJudgeLockedSign_CatchesABrokenPromise feeds the judgement a signature
// the lock reaches that was written, and one it does not reach that was
// refused for the lock. Both fail; the refusal and the signature the lock
// asks for pass.
func TestJudgeLockedSign_CatchesABrokenPromise(t *testing.T) {
	t.Parallel()
	requireQPDF(t)
	form, err := readQPDFForm(context.Background(), "qpdf", signFieldTestPDF(""))
	if err != nil {
		t.Fatal(err)
	}
	locked := fmt.Errorf("planted: %w: %w", pdf.ErrSignatureFieldUnavailable, pdf.ErrSignatureFieldLocked)
	for _, tc := range []struct {
		name string
		lock fieldLock
		err  error
		want string
	}{
		{"a locked field signed", fieldLock{present: true, action: "All"}, nil, "want ErrSignatureFieldLocked"},
		{"an unlocked field refused", fieldLock{present: true, action: "Include", fields: []string{"Name"}}, locked, "does not reach"},
		{"a locked field refused", fieldLock{present: true, action: "Exclude", fields: []string{"Name"}}, locked, ""},
		{"an unlocked field signed", fieldLock{present: true, action: "Include", fields: []string{"Name"}}, nil, ""},
	} {
		got := judgeLockedSign(form, tc.lock, "Approval", tc.err)
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestJudgeTimestampUpdate_CatchesARewrite timestamps a signature that locks
// All and checks the judgement passes it, then plants an update that rewrites
// a locked field and checks the judgement names it.
func TestJudgeTimestampUpdate_CatchesARewrite(t *testing.T) {
	t.Parallel()
	requireQPDF(t)
	ctx := context.Background()
	signer, err := sharedSigner()
	if err != nil {
		t.Fatal(err)
	}
	f := newMutationFeature(specNamed(t, "sign"))
	f.signer = signer
	trial := f.signInto(ctx, siglockFixture(t), "lockall")
	if trial.err != nil {
		t.Fatal(trial.err)
	}
	before, err := readQPDFForm(ctx, f.qpdf, trial.output)
	if err != nil {
		t.Fatal(err)
	}
	stamped, err := timestampOnce(ctx, trial.output)
	if err != nil {
		t.Fatal(err)
	}
	after, err := readQPDFForm(ctx, f.qpdf, stamped)
	if err != nil {
		t.Fatal(err)
	}
	if msg := judgeTimestampUpdate(trial.output, stamped, before, after); msg != "" {
		t.Fatalf("the real timestamp: %s", msg)
	}

	name, _ := before.field("name")
	planted := append(append([]byte(nil), stamped...), fmt.Sprintf("\n%s obj\n<< /FT /Tx /T (name) /Ff 0 >>\nendobj\n", strings.TrimSuffix(name.Object, " R"))...)
	if msg := judgeTimestampUpdate(trial.output, planted, before, after); !strings.Contains(msg, name.Object) {
		t.Errorf("a rewritten locked field: %q, want %s named", msg, name.Object)
	}
}
