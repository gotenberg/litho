package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	// Multi-call dispatch: when argv[0]'s basename matches a
	// subcommand, treat the program as that subcommand directly. The
	// provisioning Docker entrypoint cross-compiles this binary and
	// bind-mounts it at /work/bin/scan-claims, then invokes it as
	// `scan-claims --pdfs ...`. Without this dispatch the flag parser
	// would treat `--pdfs` as an unknown subcommand.
	if cmd := subcommandFromArgv0(); cmd != "" {
		os.Exit(dispatch(cmd, os.Args[1:]))
	}

	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}
	os.Exit(dispatch(os.Args[1], os.Args[2:]))
}

func subcommandFromArgv0() string {
	if filepath.Base(os.Args[0]) == "scan-claims" {
		return "scan-claims"
	}
	return ""
}

func dispatch(cmd string, args []string) int {
	switch cmd {
	case "init":
		return runInit(args)
	case "run":
		return runRun(args)
	case "baseline":
		return runBaseline(args)
	case "check":
		return runCheck(args)
	case "scan-claims":
		return runScanClaims(args)
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
// the real text instead of a rebuilt copy of it, the way cmd/litho's printUsage
// does. See [TestUsageNamesEveryDispatchableSubcommand].
func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: corpus <init|run|baseline|check|scan-claims> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  init          Provision corpora via Docker (downloads PDFs and goldens)")
	fmt.Fprintln(w, "  run           Compare litho against golden references across the matrix")
	fmt.Fprintln(w, "  baseline      Run the matrix and write the per-cell match counts to a baseline file")
	fmt.Fprintln(w, "  check         Run the matrix and fail if any cell regressed against the baseline")
	fmt.Fprintln(w, "  scan-claims   Emit a claims.csv mapping each PDF to its claimed profile")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Common run flags:")
	fmt.Fprintln(w, "  --corpus=pdfjs,verapdf        Restrict to named corpora")
	fmt.Fprintln(w, "  --feature=text,compliance     Restrict to named features")
	fmt.Fprintln(w, "  --profile=PDF_A-2b            Restrict to fixtures under one profile")
	fmt.Fprintln(w, "  --format=text|json            Output format")
	fmt.Fprintln(w, "  --workers=N                   Parallel workers (default: GOMAXPROCS)")
}
