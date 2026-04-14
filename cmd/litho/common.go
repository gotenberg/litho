package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// Exit codes.
const (
	exitOK             = 0
	exitError          = 1
	exitValidationFail = 2
	exitTimeout        = 124
)

// openPDF opens a PDF from a file path. Use "-" to read from stdin.
// The caller must close the returned Reader when done.
func openPDF(ctx context.Context, path, password string) (*pdf.Reader, func(), error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, nil, fmt.Errorf("reading stdin: %w", err)
		}
		var opts []pdf.OpenOption
		if password != "" {
			opts = append(opts, pdf.WithPassword(password))
		}
		r, err := pdf.Open(ctx, newBytesReaderAt(data), int64(len(data)), opts...)
		if err != nil {
			return nil, nil, err
		}
		return r, func() {}, nil
	}

	var opts []pdf.OpenOption
	if password != "" {
		opts = append(opts, pdf.WithPassword(password))
	}
	r, err := pdf.OpenFile(ctx, path, opts...)
	if err != nil {
		return nil, nil, err
	}
	return r, func() { r.Close() }, nil
}

// bytesReaderAt wraps a byte slice to satisfy io.ReaderAt.
type bytesReaderAt struct {
	data []byte
}

func newBytesReaderAt(data []byte) *bytesReaderAt {
	return &bytesReaderAt{data: data}
}

func (b *bytesReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b.data)) {
		return 0, io.EOF
	}
	n := copy(p, b.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// outputJSON writes v as indented JSON to w.
func outputJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// createOutput returns a writer for the given path. Use "" or "-" for stdout.
// The returned close function must be called when done.
func createOutput(path string) (io.Writer, func() error, error) {
	if path == "" || path == "-" {
		return os.Stdout, func() error { return nil }, nil
	}
	f, err := os.Create(path) //nolint:gosec // Output path comes from the -o CLI flag, not untrusted input.
	if err != nil {
		return nil, nil, fmt.Errorf("creating output file: %w", err)
	}
	return f, f.Close, nil
}

// runSimpleMutation handles commands that open a PDF, apply a single
// mutation via mutate, and write the result. The usage string is shown
// when required args are missing.
func runSimpleMutation(ctx context.Context, args []string, stderr io.Writer, cmdName, usage string, mutate func(*pdf.Writer)) int {
	fs := flag.NewFlagSet(cmdName, flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	if fs.NArg() < 1 || *output == "" {
		fmt.Fprintln(stderr, usage)
		return exitError
	}

	r, closer, err := openPDF(ctx, fs.Arg(0), *password)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}
	defer closer()

	w := pdf.NewWriter(r)
	mutate(w)

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	if err := w.WriteTo(ctx, dst); err != nil {
		closeDst() //nolint:errcheck // best-effort cleanup on write failure
		fmt.Fprintf(stderr, "writing output: %v\n", err)
		return exitError
	}

	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	return exitOK
}

// reorderArgs moves positional arguments to the end so that Go's flag
// package (which stops at the first non-flag argument) can parse all flags.
// It uses the FlagSet's registered flags to know which flags take values.
// This allows users to write:
//
//	litho remediate doc.pdf --profile PDF/A-2b -o out.pdf
//
// instead of requiring:
//
//	litho remediate --profile PDF/A-2b -o out.pdf doc.pdf
func reorderArgs(fs *flag.FlagSet, args []string) []string {
	// Build set of flag names that take values (all non-bool flags).
	valueFlags := make(map[string]bool)
	fs.VisitAll(func(f *flag.Flag) {
		if _, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok {
			valueFlags[f.Name] = true
		}
	})

	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			name := strings.TrimLeft(arg, "-")
			if strings.Contains(name, "=") {
				continue
			}
			if valueFlags[name] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			positional = append(positional, arg)
		}
	}
	return append(flags, positional...)
}

// parsePages parses a page range specification into a sorted slice of
// 1-based page numbers. Supported syntax:
//
//	"1"         → [1]
//	"1-3"       → [1, 2, 3]
//	"1,3,5"     → [1, 3, 5]
//	"1-3,7-9"   → [1, 2, 3, 7, 8, 9]
//	"even"      → [2, 4, 6, ...]
//	"odd"       → [1, 3, 5, ...]
//	"last"      → [pageCount]
func parsePages(spec string, pageCount int) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("empty page specification")
	}

	switch strings.ToLower(spec) {
	case "even":
		var pages []int
		for i := 2; i <= pageCount; i += 2 {
			pages = append(pages, i)
		}
		return pages, nil
	case "odd":
		var pages []int
		for i := 1; i <= pageCount; i += 2 {
			pages = append(pages, i)
		}
		return pages, nil
	case "last":
		if pageCount < 1 {
			return nil, fmt.Errorf("document has no pages")
		}
		return []int{pageCount}, nil
	}

	var pages []int
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid page number %q", bounds[0])
			}
			end, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid page number %q", bounds[1])
			}
			if start > end {
				return nil, fmt.Errorf("invalid range %d-%d: start > end", start, end)
			}
			for i := start; i <= end; i++ {
				pages = append(pages, i)
			}
		} else {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid page number %q", part)
			}
			pages = append(pages, n)
		}
	}

	for _, p := range pages {
		if p < 1 || p > pageCount {
			return nil, fmt.Errorf("page %d out of range [1, %d]", p, pageCount)
		}
	}

	return pages, nil
}
