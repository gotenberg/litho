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
	case "prepare":
		return runPrepare(ctx, args)
	case "fetch":
		return runFetch(ctx, args)
	case "build":
		return runBuild(ctx, args)
	case "references":
		return runReferences(ctx, args)
	case "goldens":
		return runGoldens(ctx, args)
	case "run":
		return runRun(ctx, args)
	case "check":
		return runCheck(ctx, args)
	case "baseline":
		return runBaseline(ctx, args)
	case "update":
		return runUpdate(ctx, args)
	case "versions":
		return runVersions(args)
	case "tools":
		return runTools(ctx, args)
	// Internal: the provisioning image runs provision and scan-claims, the
	// robustness feature runs convert-one.
	case "provision":
		return runProvision(ctx, args)
	case "scan-claims":
		return runScanClaims(ctx, args)
	case "convert-one":
		return runConvertOne(ctx, args)
	case "-h", "--help", "help":
		printUsage(os.Stdout)
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
	fmt.Fprintln(w, "Usage: corpus <subcommand> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "A corpus goes through four steps before it is scored; prepare runs every one")
	fmt.Fprintln(w, "it needs, skipping what is done:")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  fetch -> build -> references -> goldens -> run")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  prepare       Run every missing or stale step below, in order")
	fmt.Fprintln(w, "  fetch         Download the fixture fonts and the files of the fetched corpora (--jobs=N; --corpus=fonts: the fonts alone)")
	fmt.Fprintln(w, "  build         Generate the inputs kept as sources in git: Office and hostile documents, litho/pdf and litho/text PDFs")
	fmt.Fprintln(w, "  references    Convert the sources with the pinned Gotenberg image: the Chromium and LibreOffice PDFs the diff compares to (--fixtures=a,b)")
	fmt.Fprintln(w, "  goldens       Derive the goldens the corpora lack in the provisioning image (Docker)")
	fmt.Fprintln(w, "  run           Score litho across the (corpus, feature) matrix")
	fmt.Fprintln(w, "  check         Score, and fail if any cell regressed against the baseline")
	fmt.Fprintln(w, "  baseline      Score, and write the scores to the baseline file")
	fmt.Fprintln(w, "  update        Rewrite one corpus's committed lists from its source at its pin (--checkout=DIR)")
	fmt.Fprintln(w, "  versions      Print the pinned tool and source versions (--build-args: as docker build arguments)")
	fmt.Fprintln(w, "  tools         Compare the local verapdf, node, java, qpdf, pdfsig, fontTools and Pillow with the pins")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Every subcommand takes:")
	fmt.Fprintln(w, "  --corpus=litho/docx,*/xlsx,poi   Corpora: names, globs or sources (empty = all)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "run, check and baseline also take:")
	fmt.Fprintln(w, "  --feature=text,compliance     Restrict to named features")
	fmt.Fprintln(w, "  --profile=PDF_A-2b            Restrict to fixtures under one profile")
	fmt.Fprintln(w, "  --format=text|json            Output format")
	fmt.Fprintln(w, "  --workers=N                   Parallel workers (default: GOMAXPROCS)")
	fmt.Fprintln(w, "  --allow-version-drift         Score goldens made with other versions than versions.env pins")
	fmt.Fprintln(w, "  --report-dir=DIR              Where the converter cells draw their failures (empty: nowhere)")
	fmt.Fprintln(w, "  --oracle-sample=N             Run the targets cell's oracles on one fixture in N (default 8; 0: none)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Versions come from test/integration/corpus/versions.env; an environment variable")
	fmt.Fprintln(w, "of the same name overrides one (VERAPDF_VERSION=1.30.2 corpus prepare).")
}
