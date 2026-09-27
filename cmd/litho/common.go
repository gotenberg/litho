package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
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

// parseCommandFlags parses a command's flags and reports whether the caller
// should carry on. When it should not, exit holds the status to return.
//
// [flag.ContinueOnError] makes Parse return [flag.ErrHelp] for -h, --help and
// -help, and every command used to treat that like any other parse failure. So
// `litho info -h` printed its usage and exited 1 while `litho --help` exited 0:
// two spellings of the same request disagreeing inside one binary. The flag
// package draws the distinction itself, since [flag.ExitOnError] exits 0 on
// ErrHelp and 2 on anything else.
func parseCommandFlags(fs *flag.FlagSet, args []string) (exit int, ok bool) {
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		// Parse has already written the usage or the message to the writer the
		// command gave the FlagSet, so there is nothing left to print here.
		if errors.Is(err, flag.ErrHelp) {
			return exitOK, false
		}
		return exitError, false
	}
	return exitOK, true
}

// cliLoggerKey addresses the logger --log-level installs on the context.
type cliLoggerKey struct{}

// withCLILogger returns ctx carrying logger, for [openPDF] to hand the library.
func withCLILogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, cliLoggerKey{}, logger)
}

// cliLoggerFrom returns the logger --log-level installed, or nil.
//
// nil is the ordinary case and means the library keeps its own default, which
// discards everything: pkg/internal/core/document substitutes a NopLogger when
// no logger is supplied, so every doc.Logger call in the library is a no-op for
// a CLI run that did not ask for diagnostics.
func cliLoggerFrom(ctx context.Context) *slog.Logger {
	logger, _ := ctx.Value(cliLoggerKey{}).(*slog.Logger)
	return logger
}

// openOptions builds the options every open shares.
//
// The logger and the resource limits are threaded here rather than at the 38
// call sites, which is the whole reason openPDF exists as a chokepoint: every
// command, and each input of a multi-input command or a pipe stage, opens
// through it, and the library carries the limits into every document it
// re-reads from those.
func openOptions(ctx context.Context, password string) []pdf.OpenOption {
	var opts []pdf.OpenOption
	if password != "" {
		opts = append(opts, pdf.WithPassword(password))
	}
	if logger := cliLoggerFrom(ctx); logger != nil {
		opts = append(opts, pdf.WithLogger(logger))
	}
	if limits, ok := cliLimitsFrom(ctx); ok {
		opts = append(opts, pdf.WithLimits(limits))
	}
	return opts
}

// openPDF opens a PDF from a file path. Use "-" to read from stdin.
//
// The two routes differ in memory, which matters for a large document. A path
// streams: [pdf.OpenFile] hands the open file to the parser as an io.ReaderAt,
// so the document is never held whole. Stdin cannot do that, because a pipe has
// no size and no seek and the parser needs both, so the bytes are buffered and
// memory matches the size of the document. Prefer a path for a large file.
// extra appends options only one command takes. The caller must close the
// returned Reader when done.
func openPDF(ctx context.Context, path, password string, extra ...pdf.OpenOption) (*pdf.Reader, func(), error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, nil, fmt.Errorf("reading stdin: %w", err)
		}
		r, err := pdf.Open(ctx, newBytesReaderAt(data), int64(len(data)), append(openOptions(ctx, password), extra...)...)
		if err != nil {
			return nil, nil, err
		}
		return r, func() {}, nil
	}

	r, err := pdf.OpenFile(ctx, path, append(openOptions(ctx, password), extra...)...)
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

// reportWriter returns where a command's human-readable report belongs, given
// the path its document went to.
//
// A command that writes its document to stdout must not also print there.
// [createOutput] maps "-" and an empty path to os.Stdout, so `remediate -o -`
// appended "Profile: ..." after %%EOF, inside the bytes the caller was
// capturing, and `redact -o -` appended "Removed 0 text run(s), ...". litho
// reopens such a file without complaint, because trailing bytes after %%EOF
// are tolerated, so the contamination is silent: the caller gets a document
// that looks fine and carries text it never asked for.
//
// Diagnostics move to stderr in that case, which is the ordinary convention
// and keeps both halves: the report still reaches the caller, and the document
// on stdout stays a document. The predicate deliberately matches
// [createOutput]'s, since the two answer the same question and drifting apart
// would reopen the bug.
//
// Sixteen other document-writing commands avoid the question by printing
// nothing on success. remediate and redact are the only two that report, so
// they are the only two that have to choose a stream.
func reportWriter(outputPath string, stdout, stderr io.Writer) io.Writer {
	if outputPath == "" || outputPath == "-" {
		return stderr
	}
	return stdout
}

// abandonOutput closes a failed write and removes what it left behind.
//
// createOutput truncates the destination the moment it opens it, so a write
// that fails partway leaves a short or empty file exactly where the caller
// asked for the result, over the top of whatever was there before. A user who
// checks the exit code is safe, but the file on disk is not a PDF and looks
// like one.
//
// Writing to stdout removes nothing, since there is no file to withdraw.
//
// # Corpus evidence
//
//   - pdfjs issue19484_1.pdf: its object stream will not inflate, and
//     `litho flatten-form -o out.pdf` on it left a zero-byte out.pdf behind.
func abandonOutput(path string, closeDst func() error) {
	if closeDst != nil {
		closeDst() //nolint:errcheck // already returning an error; a close failure would mask its cause
	}
	if path == "" || path == "-" {
		return
	}
	os.Remove(path) //nolint:errcheck,gosec // best-effort withdrawal of a file that is already unusable; path is the caller's own -o flag, the same value createOutput opens above, not untrusted input
}

// releaseWriter closes w, which removes the temp file a remediation left when
// the command stopped before writing it, and warns when that file could not
// be removed. Deferred right after [pdf.NewWriter] in every command.
//
// It also reports the signatures the output does not carry (see
// [warnSignaturesDropped]). Every command that writes through a Writer defers
// this, so none can forget the warning, and a command whose write failed
// prints none: the Writer reports only on an output it wrote.
func releaseWriter(w *pdf.Writer, stderr io.Writer) {
	warnSignaturesDropped(stderr, w.DroppedSignatures())
	if err := w.Close(); err != nil {
		fmt.Fprintf(stderr, "warning: %v\n", err)
	}
}

// simpleMutationOption adjusts what [runSimpleMutation] offers a command.
type simpleMutationOption func(*simpleMutationConfig)

type simpleMutationConfig struct {
	compact               bool
	skipEncryptionWarning bool
	afterWrite            func()
}

// withAfterWrite runs report once the output is written and closed.
//
// A queued edit fills its report only when the Writer applies it, during the
// write, so a command that reports what its edit did reads the report here
// rather than in the mutate function, where the report is still empty. It does
// not run when the command fails, since the output it would describe does not
// exist.
func withAfterWrite(report func()) simpleMutationOption {
	return func(c *simpleMutationConfig) { c.afterWrite = report }
}

// withCompactFlag gives the command a -compact flag.
//
// Opt-in rather than automatic because the three commands sharing this helper
// do not agree: decrypt and flatten-form always rewrite the whole file, while
// autotag queues a mutation and leaves an incremental update. Compaction only
// runs on a full rewrite, so offering the flag to autotag would advertise an
// option that does nothing.
func withCompactFlag() simpleMutationOption {
	return func(c *simpleMutationConfig) { c.compact = true }
}

// withoutEncryptionWarning suppresses the dropped-encryption warning.
//
// Opt-OUT rather than opt-in, so the default is the safe one: a command added
// to this helper later warns about the confidentiality downgrade without
// anyone remembering to ask for it. Only decrypt uses this, because producing
// an unprotected document is precisely what it was asked to do, and reporting
// that as a loss would name the requested outcome a problem.
func withoutEncryptionWarning() simpleMutationOption {
	return func(c *simpleMutationConfig) { c.skipEncryptionWarning = true }
}

// runSimpleMutation handles commands that open a PDF, apply a single
// mutation via mutate, and write the result. The usage string is shown
// when required args are missing.
func runSimpleMutation(ctx context.Context, args []string, stderr io.Writer, cmdName, usage string, mutate func(*pdf.Writer), opts ...simpleMutationOption) int {
	var cmdCfg simpleMutationConfig
	for _, o := range opts {
		o(&cmdCfg)
	}

	fs := flag.NewFlagSet(cmdName, flag.ContinueOnError)
	fs.SetOutput(stderr)
	output := fs.String("o", "", "Output file (required)")
	password := fs.String("p", "", "PDF password")
	var compact *bool
	if cmdCfg.compact {
		compact = registerCompactFlag(fs)
	}

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
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

	// None of these commands has an encryption flag, so none can ever be
	// encrypting. decrypt opts out: see withoutEncryptionWarning.
	if !cmdCfg.skipEncryptionWarning {
		warnEncryptionDropped(stderr, false, r)
	}

	w := pdf.NewWriter(r)
	defer releaseWriter(w, stderr)
	mutate(w)

	dst, closeDst, err := createOutput(*output)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return exitError
	}

	var writeOpts []pdf.WriteOption
	if compact != nil {
		writeOpts = compactWriteOptions(*compact)
	}

	if err := w.WriteTo(ctx, dst, writeOpts...); err != nil {
		abandonOutput(*output, closeDst)
		fmt.Fprintf(stderr, "writing output: %v\n", err)
		return exitError
	}

	if err := closeDst(); err != nil {
		fmt.Fprintf(stderr, "closing output: %v\n", err)
		return exitError
	}

	if cmdCfg.afterWrite != nil {
		cmdCfg.afterWrite()
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
		// A bare "-" is the stdin marker and therefore a POSITIONAL, not a
		// flag. Filing it under flags put it AHEAD of the real flags, and
		// flag.Parse treats a lone "-" as a terminator, so everything after it
		// stopped being parsed: `litho metadata - -f json` opened "-f" as a
		// PDF. openPDF documents "-" as stdin and createOutput as stdout, so
		// this is a supported input, not an exotic one. A "-" that is a flag's
		// VALUE never reaches here, being consumed by the valueFlags branch
		// below.
		if strings.HasPrefix(arg, "-") && arg != "-" {
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

// parsePages parses a page range specification into a slice of 1-based page
// numbers, in the order the specification lists them.
//
// It does NOT sort or deduplicate: "3,1" returns [3, 1] and "1,1" returns
// [1, 1]. The comment here claimed "sorted" for a long time and the body never
// did it. Callers are unaffected because the page operations treat the list as
// a SET, measured on a three-page document: `delete --pages 1,1` removes one
// page and `delete --pages 3,1` removes both, while `rotate --pages 3,1`
// rotates pages 1 and 3 and leaves 2 alone. Sort here only if a caller appears
// that needs ordering, and give it a test that compares VALUES.
//
// Supported syntax:
//
//	"1"         → [1]
//	"1-3"       → [1, 2, 3]
//	"1,3,5"     → [1, 3, 5]
//	"1-3,7-9"   → [1, 2, 3, 7, 8, 9]
//	"even"      → [2, 4, 6, ...]
//	"odd"       → [1, 3, 5, ...]
//	"last"      → [pageCount]
//
// A specification that names no page in this document is an error, never an
// empty slice. "even" on a one-page document selects nothing, and an empty
// selection cannot be handed to the page operations: [pdf.Writer.RotatePages]
// and the three stamps document an empty variadic as EVERY page, so returning
// one silently inverts the request. Measured before this guard existed:
// `rotate -angle 90 -pages even` on a one-page file rotated page 1 and exited 0.
// Only "even" and "odd" can select nothing. A comma or range part yields at
// least one page or fails ("1,,2" fails in Atoi), and "last" already rejects a
// page-less document.
func parsePages(spec string, pageCount int) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("empty page specification")
	}

	// Callers that want every page pass no -pages flag at all and never reach
	// here, so refusing an empty selection cannot break that path.
	selected := func(pages []int) ([]int, error) {
		if len(pages) == 0 {
			return nil, fmt.Errorf("page specification %q selects no pages in a %d-page document", spec, pageCount)
		}
		return pages, nil
	}

	switch strings.ToLower(spec) {
	case "even":
		var pages []int
		for i := 2; i <= pageCount; i += 2 {
			pages = append(pages, i)
		}
		return selected(pages)
	case "odd":
		var pages []int
		for i := 1; i <= pageCount; i += 2 {
			pages = append(pages, i)
		}
		return selected(pages)
	case "last":
		if pageCount < 1 {
			return nil, fmt.Errorf("document has no pages")
		}
		return []int{pageCount}, nil
	}

	// Bounds are checked per part, BEFORE the range is expanded. The expansion
	// appends one entry per page it spans, so validating afterwards let
	// "1-10000000" build a 10-million-entry slice on a five-page document and
	// only then report that page 6 is out of range: 469 MiB and 56 ms, all of it
	// discarded, growing linearly with the endpoint until the allocation fails.
	// Anti-DoS guard, not spec-mandated. The bound is pageCount itself rather
	// than an invented constant, and the checks run in list order with endpoints
	// before their interior, which reports the same offending page the
	// post-expansion scan did: it named the first offender in expansion order,
	// and that is always an endpoint.
	inRange := func(p int) error {
		if p < 1 || p > pageCount {
			return fmt.Errorf("page %d out of range [1, %d]", p, pageCount)
		}
		return nil
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
			if err := inRange(start); err != nil {
				return nil, err
			}
			if err := inRange(end); err != nil {
				return nil, err
			}
			for i := start; i <= end; i++ {
				pages = append(pages, i)
			}
		} else {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid page number %q", part)
			}
			if err := inRange(n); err != nil {
				return nil, err
			}
			pages = append(pages, n)
		}
	}

	return pages, nil
}

// resolvePages opens path and resolves the list of page numbers to
// process. An empty pagesSpec means every page; otherwise it is parsed
// with [parsePages]. The caller must call the returned closer when the
// reader is no longer needed. On error the cause is already written to
// stderr and the reader (if any) is closed, so the caller can just
// return a non-zero exit code.
func resolvePages(ctx context.Context, path, password, pagesSpec string, stderr io.Writer) (*pdf.Reader, func(), []int, error) {
	r, closer, err := openPDF(ctx, path, password)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", path, err)
		return nil, nil, nil, err
	}

	pageCount, err := r.PageCount(ctx)
	if err != nil {
		closer()
		fmt.Fprintf(stderr, "%s: %v\n", path, err)
		return nil, nil, nil, err
	}

	var pageNums []int
	if pagesSpec != "" {
		pageNums, err = parsePages(pagesSpec, pageCount)
		if err != nil {
			closer()
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return nil, nil, nil, err
		}
	} else {
		for i := 1; i <= pageCount; i++ {
			pageNums = append(pageNums, i)
		}
	}

	return r, closer, pageNums, nil
}
