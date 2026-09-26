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
// in the body but, unless listFont is set, not in the cross-reference table.
// The table lists objects 1 to 4 and the given number of filler objects,
// numbered from 6. Text extraction resolves the font, misses the index, and
// runs the raw-byte augmentation that adds it; a validation run reads the font
// reference as a reference to an undefined object and runs no augmentation.
func unlistedFontPDF(fillers int, listFont bool) []byte {
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
	head := 4
	if listFont {
		head = 5
	}
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", head+1)
	for objNum := 1; objNum <= head; objNum++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offsets[objNum])
	}
	fmt.Fprintf(&buf, "6 %d\n", fillers)
	for objNum := 6; objNum <= len(bodies); objNum++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offsets[objNum])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(bodies)+1, start)
	return buf.Bytes()
}

// TestConcurrentValidationAndXRefAugmentation races a PDF/A validation run
// against text extraction on one [Reader] whose cross-reference table omits
// the font the page uses.
//
// Extraction resolves the font, misses the index, and runs the one-shot
// raw-byte augmentation, which writes the index. The validation run's check
// groups ranged the index map without the document lock, so extraction wrote
// the map while a group iterated it: a data race, which the runtime can also
// end with a fatal "concurrent map iteration and map write". The groups now
// read the index through a copy taken under the lock, and read a reference to
// an object the table leaves out as a reference to an undefined object, as
// veraPDF does, whether or not extraction added it, so the verdict matches a
// run nobody raced.
//
// Measured on the code before the index copy, one run of 24 trials: 7 landed
// the augmentation inside the fan-out and dropped the violation the unlisted
// font then drew, and -race reported 7 races between the augmentation's
// writes and the check groups' sweeps.
//
// The augmentation runs once per Document, so each trial opens a fresh reader,
// and extraction starts after a delay swept across the length of a serial run,
// so some trials land it inside the fan-out wherever the fan-out falls.
func TestConcurrentValidationAndXRefAugmentation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	open := func(t *testing.T, data []byte) *Reader {
		t.Helper()
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		return r
	}
	reportsFont := func(errs []string) bool {
		return slices.ContainsFunc(errs, func(s string) bool { return strings.Contains(s, "font F1") })
	}

	// Control: the font draws a violation when the table lists it, so a run
	// that reached it through the augmentation would differ from the serial
	// run below, and the trials compare something.
	listed, err := open(t, unlistedFontPDF(unlistedFontFillers, true)).ValidatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA with the font listed: %v", err)
	}
	if !reportsFont(errorStrings(listed)) {
		t.Fatalf("the listed font draws no violation among %d: %q", len(listed), errorStrings(listed))
	}

	data := unlistedFontPDF(unlistedFontFillers, false)
	started := time.Now()
	want, err := open(t, data).ValidatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("serial ValidatePDFA: %v", err)
	}
	serial := time.Since(started)
	wantStrings := errorStrings(want)
	if reportsFont(wantStrings) {
		t.Fatalf("serial run judges the font the table leaves out: %q", wantStrings)
	}

	for trial := range unlistedFontTrials {
		r := open(t, data)
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
