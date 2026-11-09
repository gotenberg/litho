package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	os.Exit(runMain())
}

// runMain dispatches the command under a context an interrupt cancels, so
// Ctrl-C reaches the workers through their contexts and a cut-short run or
// scan writes no result. It returns rather than exiting so the deferred stop runs. The first signal
// also restores the default handling: a second Ctrl-C kills a run whose
// shutdown hangs.
func runMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, stop)

	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		return 2
	}
	return dispatch(ctx, os.Args[1], os.Args[2:])
}

func dispatch(ctx context.Context, cmd string, args []string) int {
	switch cmd {
	case "init":
		return runInit(ctx, args)
	case "fetch":
		return runFetch(ctx, args)
	case "select":
		return runSelect(ctx, args)
	case "run":
		return runRun(ctx, args)
	case "baseline":
		return runBaseline(ctx, args)
	case "check":
		return runCheck(ctx, args)
	case "scan-claims":
		return runScanClaims(ctx, args)
	case "references":
		return runReferences(ctx, args)
	case "provision":
		return runProvision(ctx, args)
	case "versions":
		return runVersions(args)
	case "tools":
		return runTools(ctx, args)
	case "-h", "--help", "help":
		printUsage(os.Stderr)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", cmd)
		printUsage(os.Stderr)
		return 2
	}
}

// printUsage writes the usage block to w.
//
// Takes a writer rather than addressing os.Stderr directly so a test can read
// the real text instead of a rebuilt copy of it. See
// [TestUsageNamesEveryDispatchableSubcommand].
func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: corpus <fetch|select|init|run|baseline|check|references|scan-claims|provision|versions|tools> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  fetch         Download the fixture fonts and the files of the host-fetched corpora (--corpus=poi,wpt; --jobs=N)")
	fmt.Fprintln(w, "  select        Write a corpus's lists from a checkout of its source (--corpus=poi/docx --checkout=DIR)")
	fmt.Fprintln(w, "  references    Build the LibreOffice and Chromium references with the pinned Gotenberg image (--corpus, --names)")
	fmt.Fprintln(w, "  init          Provision corpora: their PDFs on the host, their goldens via Docker")
	fmt.Fprintln(w, "  run           Compare litho against golden references across the matrix")
	fmt.Fprintln(w, "  baseline      Run the matrix and write the per-cell match counts to a baseline file")
	fmt.Fprintln(w, "  check         Run the matrix and fail if any cell regressed against the baseline")
	fmt.Fprintln(w, "  scan-claims   Emit a claims.csv mapping each PDF to its claimed profile")
	fmt.Fprintln(w, "  provision     Derive a corpus's goldens inside the provisioning image (run by init)")
	fmt.Fprintln(w, "  versions      Print the pinned tool and source versions (--build-args: as docker build arguments)")
	fmt.Fprintln(w, "  tools         Compare the local verapdf, node, java, qpdf, pdfsig and pikepdf with the pins")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Common run flags:")
	fmt.Fprintln(w, "  --corpus=pdfjs,verapdf        Restrict to named corpora")
	fmt.Fprintln(w, "  --feature=text,compliance     Restrict to named features")
	fmt.Fprintln(w, "  --profile=PDF_A-2b            Restrict to fixtures under one profile")
	fmt.Fprintln(w, "  --format=text|json            Output format")
	fmt.Fprintln(w, "  --workers=N                   Parallel workers (default: GOMAXPROCS)")
	fmt.Fprintln(w, "  --allow-version-drift         Score goldens made with other versions than versions.env pins")
	fmt.Fprintln(w, "  --report-dir=DIR              Where the converter cells draw their failures (empty: nowhere)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Versions come from test/integration/corpus/versions.env; an environment variable")
	fmt.Fprintln(w, "of the same name overrides one (VERAPDF_VERSION=1.30.2 corpus init).")
}
