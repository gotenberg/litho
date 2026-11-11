package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/gotenberg/litho/pkg/pdf"
)

// convertOneResult is what `corpus convert-one` prints: how one conversion
// ended, read back by the robustness feature.
type convertOneResult struct {
	// Result is "pdf", the name of the sentinel the error wraps (see
	// [convertSentinels]), or "error" for any other error.
	Result string `json:"result"`
	Detail string `json:"detail,omitempty"`
	Pages  int    `json:"pages,omitempty"`
	// Fetches counts the requests the conversion tried to send.
	Fetches int64 `json:"fetches,omitempty"`
	// Leaked lists the forbidden strings the PDF's text holds.
	Leaked []string `json:"leaked,omitempty"`
	// Missing lists the required strings the PDF's text lacks.
	Missing []string `json:"missing,omitempty"`
}

// convertSentinels are the errors a conversion may end with, by the name a
// robustness expectation gives them, in the order they are matched.
var convertSentinels = []struct { //nolint:gochecknoglobals // static table
	name string
	err  error
}{
	{"panic", pdf.ErrInternalPanic},
	{"timeout", context.DeadlineExceeded},
	{"ErrConvertInputTooLarge", pdf.ErrConvertInputTooLarge},
	{"ErrConvertTooManyPages", pdf.ErrConvertTooManyPages},
	{"ErrConvertMalformed", pdf.ErrConvertMalformed},
	{"ErrConvertEncrypted", pdf.ErrConvertEncrypted},
	{"ErrConvertNoFonts", pdf.ErrConvertNoFonts},
	{"ErrUnsupportedFormat", pdf.ErrUnsupportedFormat},
	{"ErrInvalidConvertOptions", pdf.ErrInvalidConvertOptions},
}

// convertResultName names how a conversion that returned err ended.
func convertResultName(err error) string {
	if err == nil {
		return "pdf"
	}
	for _, s := range convertSentinels {
		if errors.Is(err, s.err) {
			return s.name
		}
	}
	return "error"
}

// runConvertOne converts one Office source, a document or a directory
// holding one and its options.json, and prints a [convertOneResult]. It
// runs as a child of the robustness feature, which measures its time and
// memory; it exits 0 whatever the conversion did.
func runConvertOne(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("convert-one", flag.ExitOnError)
	fonts := fs.String("fonts", "", "Font directories, separated by the OS path list separator")
	forbid := fs.String("forbid", "", "Strings, separated by newlines, the PDF's text must not hold")
	require := fs.String("require", "", "Strings, separated by newlines, the PDF's text must hold")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: corpus convert-one [--fonts=DIRS] [--forbid=TEXT] [--require=TEXT] SOURCE")
		return 2
	}
	var dirs []string
	if *fonts != "" {
		dirs = filepath.SplitList(*fonts)
	}
	res := convertOne(ctx, fs.Arg(0), dirs, textChecks{forbid: lines(*forbid), require: lines(*require)})
	if err := json.NewEncoder(os.Stdout).Encode(res); err != nil {
		fmt.Fprintf(os.Stderr, "convert-one: %v\n", err)
		return 1
	}
	return 0
}

// lines splits s at newlines, nil for an empty s.
func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// textChecks are the strings a conversion's text must not hold and must
// hold.
type textChecks struct {
	forbid, require []string
}

// convertOne converts src at the options its options.json names, counting
// every request the conversion tries and refusing it. It reads the source
// from its file and writes the PDF to a temp file, so the memory its
// parent measures is the conversion's and holds no copy of either.
//
// # Corpus evidence
//
//   - litho/stress html-png-50: the child that held its 252 MiB PDF in a
//     buffer peaked at 757 MiB alone and at 784 MiB beside twelve busy
//     loops, where `litho convert` peaks at 249 MiB either way.
func convertOne(ctx context.Context, src string, fonts []string, checks textChecks) convertOneResult {
	document, format, sourceOpts, err := loadRobustnessSource(src)
	if err != nil {
		return convertOneResult{Result: "error", Detail: err.Error()}
	}
	in, err := os.Open(document) //nolint:gosec // a fixture path
	if err != nil {
		return convertOneResult{Result: "error", Detail: err.Error()}
	}
	defer in.Close()
	out, err := os.CreateTemp("", "litho-convert-one-*.pdf")
	if err != nil {
		return convertOneResult{Result: "error", Detail: err.Error()}
	}
	defer os.Remove(out.Name())
	defer out.Close()
	var fetches atomic.Int64
	client := &http.Client{Transport: refusingTransport{&fetches}}
	opts := []pdf.ConvertOption{pdf.WithHTTPClient(client)}
	if len(fonts) > 0 {
		opts = append(opts, pdf.WithConvertFontDirs(fonts...))
	}
	opts = append(opts, sourceOpts...)
	_, err = pdf.Convert(ctx, in, format, out, opts...)
	res := convertOneResult{Result: convertResultName(err), Fetches: fetches.Load()}
	if err != nil {
		res.Detail = err.Error()
		return res
	}
	res.Pages, res.Leaked, res.Missing, err = checkText(ctx, out, checks)
	if err != nil {
		return convertOneResult{Result: "error", Detail: "read the conversion: " + err.Error(), Fetches: res.Fetches}
	}
	return res
}

// maxTextPages caps the pages whose text is checked.
const maxTextPages = 50

// checkText returns the page count of the PDF in f, the forbidden strings
// the text of its first [maxTextPages] pages holds and the required ones
// it lacks.
func checkText(ctx context.Context, f *os.File, checks textChecks) (int, []string, []string, error) {
	info, err := f.Stat()
	if err != nil {
		return 0, nil, nil, err
	}
	r, err := pdf.Open(ctx, f, info.Size())
	if err != nil {
		return 0, nil, nil, err
	}
	defer r.Close()
	n, err := r.PageCount(ctx)
	if err != nil || len(checks.forbid)+len(checks.require) == 0 {
		return n, nil, nil, err
	}
	var text strings.Builder
	for p := 1; p <= min(n, maxTextPages); p++ {
		t, err := r.ExtractText(ctx, p, pdf.WithArtifacts())
		if err != nil && !errors.Is(err, pdf.ErrRequiresOCR) {
			return n, nil, nil, err
		}
		text.WriteString(t)
	}
	var leaked, missing []string
	for _, f := range checks.forbid {
		if strings.Contains(text.String(), f) {
			leaked = append(leaked, f)
		}
	}
	for _, f := range checks.require {
		if !strings.Contains(text.String(), f) {
			missing = append(missing, f)
		}
	}
	return n, leaked, missing, nil
}

// refusingTransport counts the requests it is asked to send and sends none.
type refusingTransport struct{ n *atomic.Int64 }

func (t refusingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.n.Add(1)
	return nil, fmt.Errorf("the robustness check sends no request: %s", req.URL.Redacted())
}
