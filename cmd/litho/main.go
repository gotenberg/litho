package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gotenberg/litho/pkg/pdf"
)

// logLevels maps the --log-level values onto slog levels.
//
// An immutable lookup table, like the other package-level maps here. "off" is
// absent on purpose: it selects no logger at all rather than a level, and the
// parse handles it before this lookup.
var logLevels = map[string]slog.Level{
	"error": slog.LevelError,
	"warn":  slog.LevelWarn,
	"info":  slog.LevelInfo,
	"debug": slog.LevelDebug,
}

func main() {
	ctx, stop := interruptContext(context.Background())
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// interruptContext returns a context that the first interrupt or termination
// signal cancels, and the function that releases it.
//
// A signal used to end the process where it stood, with no deferred call run:
// a remediation interrupted while it judged its output left the
// litho-remediate-*.pdf it had written in the temp directory for good, and a
// command writing to -o left the partial file. Cancelled instead, the command
// returns through the paths a cancellation takes, which remove both. The
// first signal also restores the default behaviour, so a second one ends a
// command that does not stop at once.
func interruptContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	return ctx, stop
}

// run is the testable entry point. Every exit path returns a numeric
// status code rather than calling [os.Exit], and all output goes to the
// stdout/stderr writers passed by the caller.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		printUsage(stderr)
		return exitError
	}

	// Parse global flags that appear before the subcommand.
	// Subcommand-specific flags are parsed by each subcommand.
	cmd := ""
	var globalArgs []string
	var timeout time.Duration
	var logger *slog.Logger
	var limits pdf.Limits
	limitsSet := false

	// An index loop, not a range: --timeout consumes the argument after it,
	// and only an explicit index can step past that value. A range loop
	// advances by one whatever the body does, so the duration stayed in argv
	// and was read as the command name: the syntax printUsage itself
	// documents, "litho --timeout 30s info f.pdf", failed with "unknown
	// command: 30s" and no deadline was ever installed.
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			printUsage(stdout)
			return exitOK
		case arg == "--version":
			fmt.Fprintln(stdout, "litho", versionString())
			return exitOK
		case arg == "--timeout" || strings.HasPrefix(arg, "--timeout="):
			value, split := strings.CutPrefix(arg, "--timeout=")
			if !split {
				if i+1 >= len(args) {
					fmt.Fprintln(stderr, "--timeout needs a duration, for example --timeout 30s")
					return exitError
				}
				i++
				value = args[i]
			}
			var err error
			timeout, err = time.ParseDuration(value)
			if err != nil {
				fmt.Fprintf(stderr, "invalid timeout %q: %v\n", value, err)
				return exitError
			}
			if timeout < 0 {
				fmt.Fprintf(stderr, "invalid timeout %q: a duration must not be negative\n", value)
				return exitError
			}
		case arg == "--log-level" || strings.HasPrefix(arg, "--log-level="):
			// Consumes the next argument in the space form, so this arm steps
			// the index exactly as --timeout does above.
			value, split := strings.CutPrefix(arg, "--log-level=")
			if !split {
				if i+1 >= len(args) {
					fmt.Fprintln(stderr, "--log-level needs a level, for example --log-level debug")
					return exitError
				}
				i++
				value = args[i]
			}
			if value == "off" {
				logger = nil
				break
			}
			level, ok := logLevels[value]
			if !ok {
				fmt.Fprintf(stderr, "invalid log level %q. Valid: off, error, warn, info, debug\n", value)
				return exitError
			}
			// stderr, never stdout: a command may be streaming the document
			// there under -o -, and diagnostics mixed into it would corrupt
			// the result.
			logger = slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))
		case limitFlagFor(arg) != nil:
			// Consumes the next argument in the space form, as --timeout does.
			// A value past a budget's ceiling is refused here, with the error
			// pdf.Limits.Validate returns, which names the field and the
			// ceiling: before any document opens, and once rather than for
			// every input of a multi-input command.
			f := limitFlagFor(arg)
			value, split := strings.CutPrefix(arg, "--"+f.name+"=")
			if !split {
				if i+1 >= len(args) {
					fmt.Fprintf(stderr, "--%s needs a number, for example --%s %s\n", f.name, f.name, limitExample(f))
					return exitError
				}
				i++
				value = args[i]
			}
			if err := f.set(&limits, value); err != nil {
				fmt.Fprintf(stderr, "invalid --%s %q: %v\n", f.name, value, err)
				return exitError
			}
			// Every earlier flag passed this check, so a failure names the
			// budget this flag just set.
			if err := limits.Validate(); err != nil {
				fmt.Fprintf(stderr, "invalid --%s %q: %v\n", f.name, value, err)
				return exitError
			}
			limitsSet = true
		case len(arg) > 0 && arg[0] == '-':
			// Rejected, not skipped. An unrecognised flag used to match no
			// case and fall out of the switch, running the command as though
			// it had never been typed, so a misspelled --timout produced no
			// deadline and no complaint.
			fmt.Fprintf(stderr, "unknown global flag: %s\n\n", arg)
			printUsage(stderr)
			return exitError
		case len(arg) > 0:
			cmd = arg
			globalArgs = args[i+1:]
			goto dispatch
		}
	}

	if cmd == "" {
		printUsage(stderr)
		return exitError
	}

dispatch:
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Carried on the context rather than in a package-level variable: run is
	// called concurrently by this package's own parallel tests, and a mutable
	// global would be a data race under -race. The package-level maps here are
	// immutable lookup tables, which is a different thing.
	if logger != nil {
		ctx = withCLILogger(ctx, logger)
	}
	if limitsSet {
		ctx = withCLILimits(ctx, limits)
	}

	code := runCommand(ctx, cmd, globalArgs, stdout, stderr)

	return timeoutStatus(code, ctx.Err())
}

// timeoutStatus maps a finished command's status onto what the process
// reports, turning a failure the deadline caused into exitTimeout.
//
// exitTimeout was declared alongside the other codes and returned by nothing,
// while --timeout is an advertised flag, so an expired deadline surfaced as
// exitError and read like a corrupt file or a bad argument. 124 is the status
// timeout(1) reports, which is why a caller recognises it.
//
// Only a FAILURE converts, and that narrowness is the whole point: a run that
// produced a real verdict before the deadline expired keeps it, since
// `--timeout 1ms validate` on a non-conformant file exits 2 having reported its
// violations first, and calling that a timeout would throw the answer away.
// context.Canceled is deliberately not mapped, because this command installs no
// cancellation of its own.
//
// It is a separate function because the narrowness cannot be pinned end to
// end. An already-expired deadline makes every command fail before it can
// reach a verdict, so the case that distinguishes this rule from "any status
// becomes 124" is a race in an integration test and an ordinary table row
// here.
func timeoutStatus(code int, ctxErr error) int {
	if code == exitError && errors.Is(ctxErr, context.DeadlineExceeded) {
		return exitTimeout
	}
	return code
}

// runCommand dispatches to the subcommand cmd names.
//
// It is split from [run] so that the status every command returns passes
// through a single place, which is what lets the deadline check above see it.
// The switch returns directly from forty cases, and post-processing each
// one would be forty chances to miss one.
func runCommand(ctx context.Context, cmd string, globalArgs []string, stdout, stderr io.Writer) int {
	switch cmd {
	case "info":
		return runInfo(ctx, globalArgs, stdout, stderr)
	case "text":
		return runText(ctx, globalArgs, stdout, stderr)
	case "metadata":
		return runMetadata(ctx, globalArgs, stdout, stderr)
	case "pages":
		return runPages(ctx, globalArgs, stdout, stderr)
	case "validate":
		return runValidate(ctx, globalArgs, stdout, stderr)
	case "bookmarks":
		return runBookmarks(ctx, globalArgs, stdout, stderr)
	case "forms":
		return runForms(ctx, globalArgs, stdout, stderr)
	case "annotations":
		return runAnnotations(ctx, globalArgs, stdout, stderr)
	case "images":
		return runImages(ctx, globalArgs, stdout, stderr)
	case "signatures":
		return runSignatures(ctx, globalArgs, stdout, stderr)
	case "attachments":
		return runAttachments(ctx, globalArgs, stdout, stderr)
	case "tables":
		return runTables(ctx, globalArgs, stdout, stderr)
	case "geo":
		return runGeo(ctx, globalArgs, stdout, stderr)
	case "measurements":
		return runMeasurements(ctx, globalArgs, stdout, stderr)
	case "model3d":
		return runModel3d(ctx, globalArgs, stdout, stderr)
	case "formulas":
		return runFormulas(ctx, globalArgs, stdout, stderr)
	case "lexicons":
		return runLexicons(ctx, globalArgs, stdout, stderr)
	case "redactions":
		return runRedactions(ctx, globalArgs, stdout, stderr)
	case "rotate":
		return runRotate(ctx, globalArgs, stdout, stderr)
	case "delete":
		return runDelete(ctx, globalArgs, stdout, stderr)
	case "split":
		return runSplit(ctx, globalArgs, stdout, stderr)
	case "merge":
		return runMerge(ctx, globalArgs, stdout, stderr)
	case "insert":
		return runInsert(ctx, globalArgs, stdout, stderr)
	case "redact":
		return runRedact(ctx, globalArgs, stdout, stderr)
	case "convert-colors":
		return runConvertColors(ctx, globalArgs, stdout, stderr)
	case "flatten-transparency":
		return runFlattenTransparency(ctx, globalArgs, stdout, stderr)
	case "encrypt":
		return runEncrypt(ctx, globalArgs, stdout, stderr)
	case "decrypt":
		return runDecrypt(ctx, globalArgs, stdout, stderr)
	case "optimize":
		return runOptimize(ctx, globalArgs, stdout, stderr)
	case "set-metadata":
		return runSetMetadata(ctx, globalArgs, stdout, stderr)
	case "set-form":
		return runSetForm(ctx, globalArgs, stdout, stderr)
	case "flatten-form":
		return runFlattenForm(ctx, globalArgs, stdout, stderr)
	case "set-bookmarks":
		return runSetBookmarks(ctx, globalArgs, stdout, stderr)
	case "stamp-text":
		return runStampText(ctx, globalArgs, stdout, stderr)
	case "stamp-image":
		return runStampImage(ctx, globalArgs, stdout, stderr)
	case "stamp-pdf":
		return runStampPDF(ctx, globalArgs, stdout, stderr)
	case "sign":
		return runSign(ctx, globalArgs, stdout, stderr)
	case "timestamp":
		return runTimestamp(ctx, globalArgs, stdout, stderr)
	case "attach":
		return runAttach(ctx, globalArgs, stdout, stderr)
	case "detach":
		return runDetach(ctx, globalArgs, stdout, stderr)
	case "autotag":
		return runAutotag(ctx, globalArgs, stdout, stderr)
	case "remediate":
		return runRemediate(ctx, globalArgs, stdout, stderr)
	case "render":
		return runRender(ctx, globalArgs, stdout, stderr)
	case "contrast":
		return runContrast(ctx, globalArgs, stdout, stderr)
	case "pipe":
		return runPipe(ctx, globalArgs, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n\n", cmd)
		printUsage(stderr)
		return exitError
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: litho [--timeout DURATION] [--log-level LEVEL] [--max-LIMIT N] <command> [flags] [file...]")
	fmt.Fprintln(w)
	sections := []struct {
		title string
		rows  [][2]string
	}{
		{"Inspection:", [][2]string{
			{"info", "Quick document summary"},
			{"text", "Extract text from pages"},
			{"metadata", "Show document metadata"},
			{"pages", "List page dimensions"},
			{"validate", "Check PDF/A, PDF/UA, WTPDF or Factur-X compliance"},
			{"bookmarks", "List or auto-generate outline bookmarks"},
			{"forms", "List form fields and values"},
			{"annotations", "List annotations per page"},
			{"images", "List images per page"},
			{"signatures", "List or verify digital signatures"},
			{"attachments", "List embedded file attachments"},
			{"render", "Draw pages to PNG images, or to CMYK TIFF images"},
			{"contrast", "List text short of WCAG 2.x contrast against what lies under it (advisory)"},
		}},
		{"Extraction:", [][2]string{
			{"tables", "Extract detected tables per page"},
			{"geo", "Extract geospatial data per page"},
			{"measurements", "Extract measurement scales per page"},
			{"model3d", "List embedded 3D models per page"},
			{"formulas", "Extract MathML formulas"},
			{"lexicons", "Extract pronunciation lexicons"},
			{"redactions", "Recover text under redaction marks"},
		}},
		{"Mutation:", [][2]string{
			{"rotate", "Rotate pages"},
			{"delete", "Delete pages"},
			{"split", "Extract pages into a new PDF"},
			{"merge", "Merge multiple PDFs into one"},
			{"insert", "Insert pages from another PDF"},
			{"redact", "Remove content under page regions"},
			{"convert-colors", "Convert every colour to one colour space"},
			{"flatten-transparency", "Composite transparency into opaque images, text kept"},
			{"encrypt", "Encrypt a PDF with password"},
			{"decrypt", "Remove encryption from a PDF"},
			{"optimize", "Shrink a PDF: object streams, dedup, image recompression, font subsetting"},
			{"set-metadata", "Set document metadata fields"},
			{"set-form", "Set form field values"},
			{"flatten-form", "Flatten form fields into content"},
			{"set-bookmarks", "Replace outline from JSON or auto-generate"},
			{"stamp-text", "Stamp text on pages"},
			{"stamp-image", "Stamp an image on pages"},
			{"stamp-pdf", "Stamp a PDF page on pages"},
			{"sign", "Sign a PDF with a certificate and private key"},
			{"timestamp", "Add an RFC 3161 document timestamp"},
			{"attach", "Embed a file attachment"},
			{"detach", "Remove a file attachment"},
			{"autotag", "Synthesize a PDF/UA structure tree"},
			{"remediate", "Fix PDF/A, PDF/UA, WTPDF or Factur-X compliance issues"},
		}},
		{"Pipeline:", [][2]string{
			{"pipe", "Chain multiple operations on a PDF"},
		}},
		{"Global flags:", [][2]string{
			{"--timeout D", "Timeout duration (e.g., 30s, 5m)"},
			{"--log-level L", "Library diagnostics on stderr: off, error, warn, info, debug"},
			{"--help, -h", "Show this help"},
			{"--version", "Print the litho version"},
		}},
	}
	// Every description starts in one column, past the longest name, so a
	// long command name cannot push its own row out of line.
	width := 0
	for _, sec := range sections {
		for _, row := range sec.rows {
			width = max(width, len(row[0]))
		}
	}
	for _, sec := range sections {
		fmt.Fprintln(w, sec.title)
		for _, row := range sec.rows {
			fmt.Fprintf(w, "  %-*s  %s\n", width, row[0], row[1])
		}
		fmt.Fprintln(w)
	}
	printLimitUsage(w)
}
