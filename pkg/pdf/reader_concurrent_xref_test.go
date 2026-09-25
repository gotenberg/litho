package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/models"
)

// unlistedFontFillers is the number of filler objects [unlistedFontPDF]
// writes. They give the validation sweeps over the cross-reference index, and
// the augmentation that rewrites it, enough entries to overlap in time.
const unlistedFontFillers = 1500

// unlistedFontTrials is the number of fresh readers
// [TestConcurrentValidationAndXRefAugmentation] races validation and text
// extraction on.
const unlistedFontTrials = 24

// unlistedFontPDF assembles a one-page document whose only font, object 5, sits
// in the body but not in the cross-reference table. The table lists objects 1
// to 4 and the given number of filler objects, numbered from 6. Opening the
// file, warming its objects and building the validation context never resolve
// the font, so the raw-byte augmentation that adds it to the index runs on the
// first goroutine that does: text extraction, or the validation run once its
// check groups have returned.
func unlistedFontPDF(fillers int) []byte {
	content := "BT /F1 12 Tf 72 720 Td (Hello) Tj ET"
	bodies := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	for i := range fillers {
		bodies = append(bodies, fmt.Sprintf("<< /Filler %d >>", i))
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(bodies)+1)
	for i, body := range bodies {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}

	start := buf.Len()
	buf.WriteString("xref\n0 5\n0000000000 65535 f \r\n")
	for objNum := 1; objNum <= 4; objNum++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offsets[objNum])
	}
	fmt.Fprintf(&buf, "6 %d\n", fillers)
	for objNum := 6; objNum <= len(bodies); objNum++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offsets[objNum])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(bodies)+1, start)
	return buf.Bytes()
}

// TestConcurrentValidationAndXRefAugmentation races a PDF/A validation run
// against text extraction on one [Reader] whose cross-reference table omits
// the font the page uses.
//
// Extraction resolves the font, misses the index, and runs the one-shot
// raw-byte augmentation, which writes the index. The validation run defers
// the augmentation for its own check groups, but not for another caller, and
// its groups ranged the index map without the document lock, so extraction
// wrote the map while a group iterated it: a data race, which the runtime can
// also end with a fatal "concurrent map iteration and map write". The groups
// now read the index through a copy taken under the lock, and the run goes
// again over the final index when the augmentation changed it during the
// fan-out, so the verdict matches a run nobody raced.
//
// Measured on the code before the fix, one run of 24 trials: 7 landed the
// augmentation inside the fan-out and dropped the violation for the unlisted
// font, and -race reported 7 races between the augmentation's writes and the
// check groups' sweeps.
//
// The augmentation runs once per Document, so each trial opens a fresh reader,
// and extraction starts after a delay swept across the length of a serial run,
// so some trials land it inside the fan-out wherever the fan-out falls.
func TestConcurrentValidationAndXRefAugmentation(t *testing.T) {
	t.Parallel()

	data := unlistedFontPDF(unlistedFontFillers)
	ctx := context.Background()
	open := func(t *testing.T) *Reader {
		t.Helper()
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		return r
	}

	started := time.Now()
	want, err := open(t).ValidatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("serial ValidatePDFA: %v", err)
	}
	serial := time.Since(started)
	wantStrings := errorStrings(want)
	// Control: the serial run must report the font, which only the index the
	// augmentation completes holds, or the trials below compare nothing.
	if !slices.ContainsFunc(wantStrings, func(s string) bool { return strings.Contains(s, "font F1") }) {
		t.Fatalf("serial run reports nothing about the unlisted font among %d violations: %q", len(want), wantStrings)
	}

	for trial := range unlistedFontTrials {
		r := open(t)
		delay := serial * time.Duration(trial) / unlistedFontTrials

		var (
			wg      sync.WaitGroup
			got     []error
			valErr  error
			text    string
			textErr error
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			got, valErr = r.ValidatePDFA(ctx, models.PDFA2b)
		}()
		go func() {
			defer wg.Done()
			time.Sleep(delay)
			text, textErr = r.ExtractText(ctx, 1)
		}()
		wg.Wait()

		if valErr != nil {
			t.Fatalf("trial %d: ValidatePDFA: %v", trial, valErr)
		}
		if textErr != nil {
			t.Fatalf("trial %d: ExtractText: %v", trial, textErr)
		}
		if !strings.Contains(text, "Hello") {
			t.Errorf("trial %d: ExtractText = %q, want the text drawn with the unlisted font", trial, text)
		}
		if gotStrings := errorStrings(got); !slices.Equal(gotStrings, wantStrings) {
			t.Errorf("trial %d (extraction after %v): %d violations, want the %d of a serial run\n got: %q\nwant: %q",
				trial, delay, len(gotStrings), len(wantStrings), gotStrings, wantStrings)
		}
	}
}

// errorStrings renders errs in order.
func errorStrings(errs []error) []string {
	out := make([]string, len(errs))
	for i, err := range errs {
		out[i] = err.Error()
	}
	return out
}
