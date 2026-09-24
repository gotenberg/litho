package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
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
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
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
// The switch returns directly from thirty-nine cases, and post-processing each
// one would be thirty-nine chances to miss one.
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
	case "encrypt":
		return runEncrypt(ctx, globalArgs, stdout, stderr)
	case "decrypt":
		return runDecrypt(ctx, globalArgs, stdout, stderr)
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
	case "attach":
		return runAttach(ctx, globalArgs, stdout, stderr)
	case "detach":
		return runDetach(ctx, globalArgs, stdout, stderr)
	case "autotag":
		return runAutotag(ctx, globalArgs, stdout, stderr)
	case "remediate":
		return runRemediate(ctx, globalArgs, stdout, stderr)
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
	fmt.Fprintln(w, "Inspection:")
	fmt.Fprintln(w, "  info          Quick document summary")
	fmt.Fprintln(w, "  text          Extract text from pages")
	fmt.Fprintln(w, "  metadata      Show document metadata")
	fmt.Fprintln(w, "  pages         List page dimensions")
	fmt.Fprintln(w, "  validate      Check PDF/A or PDF/UA compliance")
	fmt.Fprintln(w, "  bookmarks     List or auto-generate outline bookmarks")
	fmt.Fprintln(w, "  forms         List form fields and values")
	fmt.Fprintln(w, "  annotations   List annotations per page")
	fmt.Fprintln(w, "  images        List images per page")
	fmt.Fprintln(w, "  signatures    List or verify digital signatures")
	fmt.Fprintln(w, "  attachments   List embedded file attachments")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Extraction:")
	fmt.Fprintln(w, "  tables        Extract detected tables per page")
	fmt.Fprintln(w, "  geo           Extract geospatial data per page")
	fmt.Fprintln(w, "  measurements  Extract measurement scales per page")
	fmt.Fprintln(w, "  model3d       List embedded 3D models per page")
	fmt.Fprintln(w, "  formulas      Extract MathML formulas")
	fmt.Fprintln(w, "  lexicons      Extract pronunciation lexicons")
	fmt.Fprintln(w, "  redactions    Recover text under redaction marks")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Mutation:")
	fmt.Fprintln(w, "  rotate        Rotate pages")
	fmt.Fprintln(w, "  delete        Delete pages")
	fmt.Fprintln(w, "  split         Extract pages into a new PDF")
	fmt.Fprintln(w, "  merge         Merge multiple PDFs into one")
	fmt.Fprintln(w, "  insert        Insert pages from another PDF")
	fmt.Fprintln(w, "  redact        Remove content under page regions")
	fmt.Fprintln(w, "  encrypt       Encrypt a PDF with password")
	fmt.Fprintln(w, "  decrypt       Remove encryption from a PDF")
	fmt.Fprintln(w, "  set-metadata  Set document metadata fields")
	fmt.Fprintln(w, "  set-form      Set form field values")
	fmt.Fprintln(w, "  flatten-form  Flatten form fields into content")
	fmt.Fprintln(w, "  set-bookmarks Replace outline from JSON or auto-generate")
	fmt.Fprintln(w, "  stamp-text    Stamp text on pages")
	fmt.Fprintln(w, "  stamp-image   Stamp an image on pages")
	fmt.Fprintln(w, "  stamp-pdf     Stamp a PDF page on pages")
	fmt.Fprintln(w, "  sign          Sign a PDF (stub, requires custom Signer)")
	fmt.Fprintln(w, "  attach        Embed a file attachment")
	fmt.Fprintln(w, "  detach        Remove a file attachment")
	fmt.Fprintln(w, "  autotag       Synthesize a PDF/UA structure tree")
	fmt.Fprintln(w, "  remediate     Fix PDF/A or PDF/UA compliance issues")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Pipeline:")
	fmt.Fprintln(w, "  pipe          Chain multiple operations on a PDF")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Global flags:")
	fmt.Fprintln(w, "  --timeout D   Timeout duration (e.g., 30s, 5m)")
	fmt.Fprintln(w, "  --log-level L Library diagnostics on stderr: off, error, warn, info, debug")
	fmt.Fprintln(w, "  --help, -h    Show this help")
	fmt.Fprintln(w)
	printLimitUsage(w)
}
