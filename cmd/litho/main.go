package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

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

	for i, arg := range args {
		switch {
		case arg == "--help" || arg == "-h":
			printUsage(stdout)
			return exitOK
		case arg == "--timeout" && i+1 < len(args):
			var err error
			timeout, err = time.ParseDuration(args[i+1])
			if err != nil {
				fmt.Fprintf(stderr, "invalid timeout %q: %v\n", args[i+1], err)
				return exitError
			}
			// Skip next arg (the duration value).
			continue
		case len(arg) > 0 && arg[0] != '-':
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
	fmt.Fprintln(w, "Usage: litho [--timeout DURATION] <command> [flags] [file...]")
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
	fmt.Fprintln(w, "  remediate     Fix PDF/A or PDF/UA compliance issues")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Pipeline:")
	fmt.Fprintln(w, "  pipe          Chain multiple operations on a PDF")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Global flags:")
	fmt.Fprintln(w, "  --timeout D   Timeout duration (e.g., 30s, 5m)")
	fmt.Fprintln(w, "  --help, -h    Show this help")
}
