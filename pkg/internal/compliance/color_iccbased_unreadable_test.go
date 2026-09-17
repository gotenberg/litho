package compliance

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// iccProfileBytes builds a minimal but well-formed ICC profile: a 128-byte
// header per ICC.1 Table 14 followed by a zero tag count. major sets the
// version byte ISO 19005-1 §6.2.3.2 t1 caps below 3.
func iccProfileBytes(colorSpace string, major byte) string {
	b := make([]byte, 132)
	copy(b[4:8], "ADBE")
	b[8] = major
	b[9] = 0x20
	copy(b[12:16], "mntr")
	copy(b[16:20], colorSpace)
	copy(b[20:24], "XYZ ")
	copy(b[36:40], "acsp")
	binary.BigEndian.PutUint32(b[0:4], uint32(len(b)))
	return string(b)
}

// iccDocObjs builds a one-page document painting in an ICCBased colour space.
// csObj is object 5, the colour space array, and iccObj object 6, the profile
// it names. Passing an empty iccObj leaves object 6 absent.
func iccDocObjs(csObj, iccObj string) []string {
	const content = "/CS0 cs 0.1 0.2 0.3 sc 0 0 50 50 re f"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] " +
			"/Resources << /ColorSpace << /CS0 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		csObj,
	}
	if iccObj != "" {
		objs = append(objs, iccObj)
	}
	return objs
}

// iccNStreamObj wraps profile bytes in a stream carrying the given /N.
func iccNStreamObj(n int, profile string) string {
	return fmt.Sprintf("<< /N %d /Length %d >>\nstream\n%s\nendstream", n, len(profile), profile)
}

// countICCClauseViolations reports how many violations carry the given clause
// and test number under the given profile.
func countICCClauseViolations(t *testing.T, objs []string, profile models.PDFAProfile, clause string, test int) int {
	t.Helper()

	data := cyclePDF(objs...)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}

	n := 0
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == clause && v.Rule.TestNumber == test {
			n++
		}
	}
	return n
}

// countUnlabelledViolations reports violations carrying a zero Rule.
//
// Reporting through a part that states no such requirement produces one, and a
// clause-keyed count never sees it: the zero Rule carries an empty clause and
// matches no clause a test would ask for.
func countUnlabelledViolations(t *testing.T, objs []string, profile models.PDFAProfile) int {
	t.Helper()

	data := cyclePDF(objs...)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}

	n := 0
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == "" {
			n++
		}
	}
	return n
}

// TestICCBasedUnreadableProfileStillJudgesN pins ISO 19005-1 §6.2.3.2 t2 across
// every way an embedded profile can fail to yield a component count.
//
// A profile that will not decode, or whose header stops before the data colour
// space at byte 16, states no component count, so /N cannot match it and the
// requirement stands unmet. Reporting only t1 there left t2 unreachable: the
// rule existed and the check was simply never run. veraPDF 1.30.2 reports both
// on these same inputs, naming a null colour space.
//
// An unresolvable profile stream is deliberately excluded. There is then no
// profile object to judge at all, and veraPDF reports neither test.
func TestICCBasedUnreadableProfileStillJudgesN(t *testing.T) {
	t.Parallel()

	good := iccProfileBytes("RGB ", 2)

	for _, tc := range []struct {
		name         string
		csObj        string
		iccObj       string
		wantT1       int
		wantT2       int
		wantT2Reason string
	}{
		{
			name:         "header too short",
			csObj:        "[/ICCBased 6 0 R]",
			iccObj:       iccNStreamObj(3, "\x00\x00\x00\x05\x00"),
			wantT1:       1,
			wantT2:       1,
			wantT2Reason: "a truncated header states no colour space",
		},
		{
			name:  "undecodable stream",
			csObj: "[/ICCBased 6 0 R]",
			iccObj: fmt.Sprintf("<< /N 3 /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream",
				len("not-valid-zlib-data!!"), "not-valid-zlib-data!!"),
			wantT1:       1,
			wantT2:       1,
			wantT2Reason: "a profile that will not decode states no colour space",
		},
		{
			name:         "unresolvable stream",
			csObj:        "[/ICCBased 99 0 R]",
			iccObj:       "",
			wantT1:       1,
			wantT2:       0,
			wantT2Reason: "no profile object exists to carry a component count",
		},
		{
			name:         "profile object is not a stream",
			csObj:        "[/ICCBased 6 0 R]",
			iccObj:       "<< /N 3 >>",
			wantT1:       1,
			wantT2:       0,
			wantT2Reason: "no profile object exists to carry a component count",
		},
		{
			name:         "well formed, N matches",
			csObj:        "[/ICCBased 6 0 R]",
			iccObj:       iccNStreamObj(3, good),
			wantT1:       0,
			wantT2:       0,
			wantT2Reason: "/N=3 matches an RGB profile",
		},
		{
			name:         "well formed, N mismatched",
			csObj:        "[/ICCBased 6 0 R]",
			iccObj:       iccNStreamObj(4, good),
			wantT1:       0,
			wantT2:       1,
			wantT2Reason: "/N=4 contradicts an RGB profile",
		},
		{
			name:         "missing N entry",
			csObj:        "[/ICCBased 6 0 R]",
			iccObj:       fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(good), good),
			wantT1:       0,
			wantT2:       1,
			wantT2Reason: "an absent /N matches nothing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			objs := iccDocObjs(tc.csObj, tc.iccObj)
			if got := countICCClauseViolations(t, objs, models.PDFA1b, "6.2.3.2", 1); got != tc.wantT1 {
				t.Errorf("§6.2.3.2 t1 fired %d time(s), want %d", got, tc.wantT1)
			}
			if got := countICCClauseViolations(t, objs, models.PDFA1b, "6.2.3.2", 2); got != tc.wantT2 {
				t.Errorf("§6.2.3.2 t2 fired %d time(s), want %d (%s)", got, tc.wantT2, tc.wantT2Reason)
			}
		})
	}
}

// TestICCBasedUnreadableProfileSilentBeyondPart1 pins the component-count rule
// to ISO 19005-1.
//
// Parts 2 to 4 state no equivalent requirement (see [iccBasedNRule]), so an
// unreadable profile there reports the profile rule alone. Without this, the
// new report would have been labelled with a requirement those parts do not
// carry.
func TestICCBasedUnreadableProfileSilentBeyondPart1(t *testing.T) {
	t.Parallel()

	objs := iccDocObjs("[/ICCBased 6 0 R]", iccNStreamObj(3, "\x00\x00\x00\x05\x00"))

	for _, tc := range []struct {
		name    string
		profile models.PDFAProfile
	}{
		{"part 2", models.PDFA2b},
		{"part 3", models.PDFA3b},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := countICCClauseViolations(t, objs, tc.profile, "6.2.4.2", 1); got == 0 {
				t.Errorf("§6.2.4.2 t1 did not fire on an unreadable profile")
			}
			if got := countICCClauseViolations(t, objs, tc.profile, "6.2.4.2", 2); got != 0 {
				t.Errorf("§6.2.4.2 t2 fired %d time(s) on an unreadable profile, want 0", got)
			}
			if got := countUnlabelledViolations(t, objs, tc.profile); got != 0 {
				t.Errorf("%d violation(s) carried an empty rule, want 0: a part stating no such "+
					"requirement must stay silent rather than report through a zero Rule", got)
			}
		})
	}
}
