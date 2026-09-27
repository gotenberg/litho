package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// pipeUsage writes the pipe command's own usage. pipe parses its arguments by
// hand rather than owning a FlagSet, so this stands in for the usage a FlagSet
// would print for any other command.
func pipeUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: litho pipe <file> <stage> [stage-flags] ... [--compact] [--strict] -o out.pdf")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  --compact   Shrink the output by packing objects into object streams")
	fmt.Fprintln(w, "              and merging duplicate streams. Applies to the final write,")
	fmt.Fprintln(w, "              and only when a stage rewrites the whole file.")
	fmt.Fprintln(w, "  --strict    Exit 2 when a remediate stage leaves violations it could")
	fmt.Fprintln(w, "              not fix. The output is still written.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Stages: rotate, split, merge, delete, insert, encrypt, decrypt,")
	fmt.Fprintln(w, "        autotag, set-metadata, set-form, flatten-form,")
	fmt.Fprintln(w, "        set-bookmarks, attach, detach, stamp-text, stamp-image,")
	fmt.Fprintln(w, "        stamp-pdf, remediate")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "remediate runs at most once, as the last stage, and a PDF/A profile")
	fmt.Fprintln(w, "takes no encrypt stage.")
}

// runPipe implements `litho pipe <file> <stage> [flags] <stage> [flags] ... -o out.pdf`.
// Stages reuse the same names and flags as individual mutation commands.
func runPipe(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	// pipe owns no FlagSet, so the flag.ErrHelp path the other commands take
	// through parseCommandFlags never runs here: `litho pipe -h` fell into the
	// "too few arguments" arm below and exited 1 while `litho info -h` exited
	// 0. Only the LEADING token counts, because a -h further along may be a
	// flag's value rather than a request, and the per-flag arity needed to tell
	// those apart is exactly what splitStageGroups documents it lacks.
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "-help") {
		pipeUsage(stderr)
		return exitOK
	}
	if len(args) < 2 {
		pipeUsage(stderr)
		return exitError
	}

	// Extract -o, -p, and input file before stage parsing.
	inputFile, password, outputFile, compact, strict, stageArgs := parsePipeGlobals(args)
	if inputFile == "" {
		fmt.Fprintln(stderr, "pipe: missing input file")
		return exitError
	}

	// Split stageArgs into groups: each group starts with a stage name.
	groups := splitStageGroups(stageArgs)

	// Checked before the input is opened or any stage runs, so a pipeline the
	// library would refuse partway through fails as a usage error instead.
	if err := validateStageOrder(groups); err != nil {
		fmt.Fprintf(stderr, "pipe: %v\n", err)
		return exitError
	}

	r, closer, err := openPDF(ctx, inputFile, password)
	if err != nil {
		fmt.Fprintf(stderr, "pipe: %v\n", err)
		return exitError
	}
	defer closer()

	pipeline := pdf.NewPipeline().Open(r)

	if len(groups) == 0 {
		fmt.Fprintln(stderr, "pipe: no stages specified")
		return exitError
	}

	// An encrypt stage makes the final output encrypted, which the write
	// engine refuses to compact. splitStageGroups guarantees the first token
	// of a group is the stage name, so this reads the pipeline rather than
	// guessing at the argument list.
	encryptsOutput, dropsEncryption, dropsXFA := false, false, false
	var compactRefusedProfile string
	for _, g := range groups {
		if len(g) == 0 {
			continue
		}
		if g[0] == "encrypt" {
			encryptsOutput = true
		}
		if !silentStages[g[0]] {
			dropsEncryption = true
		}
		if xfaDroppingStages[g[0]] {
			dropsXFA = true
		}
		// A remediate stage names the profile the caller asked for, and
		// PDF/A-1 forbids the cross-reference stream that packing requires.
		// The standalone command refuses --compact there; pipe honoured it and
		// wrote a document that no longer conformed to the profile it had just
		// remediated to, exit 0 and silent. Measured on one file: ISO 19005-1,
		// 6.1.4 test 3, exactly one violation the uncompacted pipeline lacks.
		if g[0] == "remediate" {
			if p := stageProfileValue(g); compactForbiddenByProfile(p) {
				compactRefusedProfile = p
			}
		}
	}
	warnCompactSkipped(stderr, compact, encryptsOutput)

	// Reported after the encryption case and applied after both warnings, so
	// each one still judges the flag as the caller passed it.
	warnCompactProfileRefused(stderr, compact, compactRefusedProfile)
	compact = compact && !compactForbiddenByProfile(compactRefusedProfile)

	// Residual violations left by any remediate stage, judged after the write
	// when --strict was passed. Stages run sequentially, pdf.Pipeline.WriteTo
	// iterating its stage list with no goroutines, so a stage closure appending
	// here needs no synchronisation.
	var remediationResidual []error

	// The report of every autotag stage, in stage order, read after the write
	// because the Writer fills each one when it applies the edit. Appended by
	// the stage closures under the same sequential execution as the residuals.
	var autoTagReports []*pdf.AutoTagReport

	// Documents a page-operation stage opened for itself, so the encryption
	// warning below can see them. merge has many sources and insert has two
	// inputs, and any encrypted one means the output loses protection it had.
	var stageReaders []*pdf.Reader

	// Documents a stage opened for itself, such as the merge sources. They stay
	// live until WriteTo executes the stage that reads them, so they are closed
	// here rather than in addStage.
	var stageClosers []func()
	defer func() {
		for _, c := range stageClosers {
			c()
		}
	}()

	for _, group := range groups {
		stageName := group[0]
		stageFlags := group[1:]

		closers, err := addStage(ctx, pipeline, r, stageName, stageFlags, password, stderr, &remediationResidual, &stageReaders, &autoTagReports)
		stageClosers = append(stageClosers, closers...)
		if err != nil {
			// Asking a stage for its flags is a request, not a failure. The
			// stage FlagSet has already written its usage to stderr, and
			// wrapping it in a "pipe stage" error would report a help request
			// as a broken command.
			if errors.Is(err, flag.ErrHelp) {
				return exitOK
			}
			fmt.Fprintf(stderr, "pipe stage %q: %v\n", stageName, err)
			return exitError
		}
	}

	// The same confidentiality downgrade the standalone page operations report,
	// which pipe performed silently: `litho delete -p pw` on an encrypted file
	// warns, while `litho pipe -p pw ... delete` wrote the identical plaintext
	// output and said nothing. Emitted before the write, so it is seen even if
	// the write later fails, exactly as the standalone commands do.
	if dropsEncryption {
		readers := append([]*pdf.Reader{r}, stageReaders...)
		// An encrypt stage is refused beside a PDF/A remediation, so the
		// warning must not offer one.
		if p := remediateProfile(groups); pdfaProfiles[p] != "" {
			warnEncryptionDroppedByPDFA(stderr, encryptsOutput, readers...)
		} else {
			warnEncryptionDroppedAddingEncryptStage(stderr, encryptsOutput, readers...)
		}
	}

	// The same form-data loss the standalone split and merge report, which pipe
	// performed silently: `litho split` on an XFA document warns, while
	// `litho pipe ... split` wrote the identical output and said nothing.
	// Measured with pdf.Reader.HasXFAForm on the output of every stage, not
	// by scanning bytes, because a byte scan cannot see an /ObjStm.
	//
	// Pipe already writes the encryption warning to this stream, so the "pipe
	// prints nothing but the document" rule that discards a remediate report
	// does not apply: the channel was here and only this notice was missing.
	if dropsXFA {
		warnXFADropped(ctx, stderr, append([]*pdf.Reader{r}, stageReaders...)...)
	}

	if err := pipeline.Validate(); err != nil {
		fmt.Fprintf(stderr, "pipe: %v\n", err)
		return exitError
	}

	// Checked here rather than beside the input file, because the stage loop
	// above answers a help request by returning exitOK and a help request
	// carries no -o: `litho pipe doc.pdf attach -h` reported "-o output file is
	// required" and exited 1, so the ErrHelp branch that exists to serve stage
	// help was unreachable in the natural argument order. Nothing between that
	// check's old home and this point can succeed and exit, so every genuinely
	// missing -o still fails here with the same message and status.
	if outputFile == "" {
		fmt.Fprintln(stderr, "pipe: -o output file is required")
		return exitError
	}

	dst, closeDst, err := createOutput(outputFile)
	if err != nil {
		fmt.Fprintf(stderr, "pipe: %v\n", err)
		return exitError
	}

	// The per-stage report is diagnostic only and this command prints nothing
	// but the output file, so discard it in the assignment rather than binding
	// a name with no use.
	// Pipeline.WriteTo forwards these to the final document write: the
	// executor keeps them as finalOpts for a split or merge stage, and the
	// terminal flush passes them straight to Writer.WriteTo. They do not
	// reach the intermediate temp files, which is correct, since compacting
	// a spill that the next stage reopens and rewrites is wasted work.
	_, err = pipeline.WriteTo(ctx, dst, compactWriteOptions(compact)...)
	if err != nil {
		// Withdraw the file createOutput already made. Every other
		// output-producing command in this package does this on a failed write
		// and pipe did not, so a stage that failed during the write left a
		// partial document on disk beside a non-zero exit: the caller sees the
		// failure and still finds something that looks like a result. A stage
		// that fails while it is being BUILT never reaches here, which is why
		// only the write-time failures leaked.
		abandonOutput(outputFile, closeDst)
		fmt.Fprintf(stderr, "pipe: %v\n", err)
		return exitError
	}

	if closeErr := closeDst(); closeErr != nil {
		fmt.Fprintf(stderr, "pipe: closing output: %v\n", closeErr)
		return exitError
	}

	// The standalone autotag command reports a skip, so the stage does too.
	// Pipe writes its other warnings to this stream, so the "pipe prints
	// nothing but the document" rule, which concerns stdout, does not apply.
	for _, report := range autoTagReports {
		warnAutoTagSkipped(stderr, report)
	}

	// A remediate stage asserts the profile in the output whatever it could
	// not repair, exactly as the standalone command does, so a pipeline that
	// ends in one reports success for a document that does not conform.
	// --strict answers on the exit code alone: pipe prints nothing but the
	// document, and -o - streams that document to stdout, so a report written
	// there would corrupt the result.
	if strict && len(fatalResiduals(&pdf.RemediationReport{Residual: remediationResidual})) > 0 {
		return exitValidationFail
	}

	return exitOK
}

// parsePipeGlobals extracts input file, -p, -o from args, returning the
// remaining args as stage arguments.
//
// A token consumed as a stage flag's VALUE is never examined as a global.
// Without that, `set-metadata -title -o out.pdf` read the title's own value as
// the output flag and took the token after it as the filename, leaving the real
// -o to fall through to the stage: the command exited 0 having written a file
// named "-o" into the working directory, with the title set to a path.
//
// A look-behind ("was the previous token a flag?") cannot express this, because
// the legitimate -o also follows a flag. The token consumed as a value has to be
// marked consumed, so the one after it is examined normally.
func parsePipeGlobals(args []string) (inputFile, password, outputFile string, compact, strict bool, rest []string) {
	consumed := false
	for i := 0; i < len(args); i++ {
		if consumed {
			consumed = false
			rest = append(rest, args[i])
			continue
		}
		switch {
		case args[i] == "-o" && i+1 < len(args):
			outputFile = args[i+1]
			i++
		case args[i] == "-p" && i+1 < len(args):
			password = args[i+1]
			i++
		// A global boolean, so it advances nothing and must not set
		// consumed: -o and -p step over their value, this one has none, and
		// marking it consumed would swallow the following token the way a
		// stage flag's value once swallowed the real -o.
		//
		// It is also deliberately NOT in boolStageFlags. That map keeps a
		// boolean STAGE flag from eating the next stage name, and listing a
		// global there would mis-split the stage groups.
		case args[i] == "-compact" || args[i] == "--compact":
			compact = true
		// The same shape as -compact, for the same reasons: a global boolean
		// that advances nothing, must not set consumed, and stays out of
		// boolStageFlags. No stage defines -strict, so it collides with no
		// stage flag spelling.
		case args[i] == "-strict" || args[i] == "--strict":
			strict = true
		case inputFile == "" && !strings.HasPrefix(args[i], "-"):
			inputFile = args[i]
		default:
			rest = append(rest, args[i])
			consumed = expectsValue(args[i])
		}
	}
	return
}

// splitStageGroups splits args into groups where each group starts with
// a known stage name.
//
// A token in a flag's VALUE position is never a stage boundary, however much it
// looks like one. Every name in [stageCmds] is an ordinary word a caller may
// pass as a title or a stamp: `set-metadata -title rotate` used to split after
// -title, leaving the stage holding a flag with no argument and failing the
// whole command with "flag needs an argument: -title". Every name in the set
// broke that way, while the --flag=value form was unaffected, which made it a
// confusing failure to hit. Stated without a count on purpose: the set has
// grown once already, and a number here goes stale silently.
//
// One sharp edge remains, because removing it needs per-flag arity the splitter
// does not have: merge takes POSITIONAL file arguments, so a file literally
// named for a stage still starts a new group.
func splitStageGroups(args []string) [][]string {
	var groups [][]string
	var current []string

	for i, arg := range args {
		if isStageCmd(arg) && (i == 0 || !expectsValue(args[i-1])) {
			if len(current) > 0 {
				groups = append(groups, current)
			}
			current = []string{arg}
		} else {
			current = append(current, arg)
		}
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}
	return groups
}

// boolStageFlags names every stage flag that takes no value.
//
// A value-expecting flag consumes the token after it, so that token cannot be a
// stage. A boolean consumes nothing, so the token after it is a genuine stage
// and must still start a group: `remediate -profile PDF/A-2b -aggressive rotate`
// runs two stages, not one.
//
// Every boolean a stage defines has to be listed here, and the cost of
// forgetting one is silent: `set-bookmarks -generate rotate -angle 90` would
// treat `rotate` as -generate's value, run ONE stage instead of two, and exit 0
// having never rotated anything. Adding a boolean stage flag means adding it
// here in the same change.
//
// h and help are here although no stage declares them: every FlagSet answers
// them implicitly, and they take no value. Without them `pipe doc attach -h -o
// out.pdf` consumed the -o as -h's value and reported "-o output file is
// required" for a command that supplied one, and `set-metadata -title -h
// rotate` swallowed the rotate stage. TrimLeft strips every leading dash, so
// these two entries cover -h, -help and --help.
var boolStageFlags = map[string]bool{
	"aggressive": true, "generate": true,
	"h": true, "help": true,
}

// expectsValue reports whether arg is a stage flag that consumes the next
// token. The --flag=value form carries its own value and consumes nothing.
func expectsValue(arg string) bool {
	if !strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
		return false
	}
	return !boolStageFlags[strings.TrimLeft(arg, "-")]
}

// validateStageOrder refuses a stage order the library would refuse partway
// through, before pipe opens anything.
//
// A remediation is the last edit a document takes: its report describes the
// output only when nothing edits it afterwards, and a [pdf.Writer] refuses
// every edit after one, a second remediation included. So remediate must be
// the last stage, and there is one at most. Every stage after it is named,
// so the caller can move them in one go.
//
// PDF/A forbids encryption: ISO 19005-1, 6.1.3, ISO 19005-2, 6.1.3, ISO
// 19005-3, 6.1.3 and ISO 19005-4, 6.1.3 each keep the Encrypt key out of the
// trailer, so an encrypt stage cannot precede a PDF/A remediate stage. A
// PDF/UA or WTPDF remediation keeps it.
func validateStageOrder(groups [][]string) error {
	remediateAt := -1
	for i, g := range groups {
		if len(g) == 0 || g[0] != "remediate" {
			continue
		}
		if remediateAt >= 0 {
			return fmt.Errorf("only one remediate stage is allowed: a remediation is the last edit, and a second one would edit the output the first one reported on")
		}
		remediateAt = i
	}
	if remediateAt < 0 {
		return nil
	}
	var after []string
	for _, g := range groups[remediateAt+1:] {
		if len(g) > 0 {
			after = append(after, fmt.Sprintf("%q", g[0]))
		}
	}
	if len(after) > 0 {
		return fmt.Errorf("remediate must be the last stage, since its report describes the output only when nothing edits it afterwards: move %s before remediate", strings.Join(after, ", "))
	}
	profile := stageProfileValue(groups[remediateAt])
	if pdfaProfiles[profile] == "" {
		return nil
	}
	for _, g := range groups[:remediateAt] {
		if len(g) > 0 && g[0] == "encrypt" {
			return fmt.Errorf("an \"encrypt\" stage cannot precede remediate -profile %s: %s forbids encryption in a PDF/A file", profile, pdfaEncryptionClause(profile))
		}
	}
	return nil
}

// remediateProfile returns the --profile of the pipeline's remediate stage,
// or "" when it has none.
func remediateProfile(groups [][]string) string {
	for _, g := range groups {
		if len(g) > 0 && g[0] == "remediate" {
			return stageProfileValue(g)
		}
	}
	return ""
}

// pdfaEncryptionClause names the clause of the ISO 19005 part of profile, a
// PDF/A profile name, that forbids the Encrypt key in the trailer: 6.1.3 in
// every part.
func pdfaEncryptionClause(profile string) string {
	part, _ := strings.CutPrefix(profile, "PDF/A-")
	if part == "" {
		return "ISO 19005, 6.1.3"
	}
	return "ISO 19005-" + part[:1] + ", 6.1.3"
}

// stageProfileValue returns the --profile a stage group carries, or "".
//
// All four spellings, because the CLI accepts all four: `-profile X`,
// `--profile X`, `-profile=X` and `--profile=X`. [splitStageGroups] keeps a
// stage's flags verbatim in its group, so the value is readable here without
// the stage's own FlagSet, which addStage builds later and keeps inside a
// closure runPipe cannot reach.
func stageProfileValue(group []string) string {
	for i, arg := range group {
		name, value, hasEq := strings.Cut(arg, "=")
		if name != "-profile" && name != "--profile" {
			continue
		}
		if hasEq {
			return value
		}
		if i+1 < len(group) {
			return group[i+1]
		}
	}
	return ""
}

// stageCmds names every command that also runs as a pipeline stage.
//
// Two output-producing commands are deliberately absent, so that this set reads
// as a decision rather than an oversight:
//
// sign, because a signature covers the bytes it was computed over. A stage
// after it rewrites the document and voids it, and pipe cannot guarantee it
// runs last, so the honest surface is the standalone command.
//
// redact, because its value is the report: RedactReport names the copies that
// survived and the objects it could not prove clean, and Redact refuses with
// [pdf.ErrRedactionIncomplete] rather than writing a half-cleared file. pipe
// prints nothing but the document (-o - streams it to stdout), so a redact
// stage would either discard that report or corrupt the output by printing it.
// Weakening a security contract to gain a stage is the wrong trade.
var stageCmds = map[string]bool{
	"rotate": true, "split": true, "merge": true, "delete": true,
	"insert": true, "encrypt": true, "decrypt": true, "set-metadata": true,
	"set-form": true, "flatten-form": true, "set-bookmarks": true,
	"attach": true, "detach": true, "stamp-text": true,
	"stamp-image": true, "stamp-pdf": true, "remediate": true, "autotag": true,
}

func isStageCmd(s string) bool { return stageCmds[s] }

// silentStages names the stages whose standalone command deliberately does NOT
// warn that an encrypted input yields an unprotected output.
//
// A DENY list, and the inversion is the point. This was an allow list of the
// five page operations, correct when `b59fc758` wrote it because only those
// five warned. `bbed371f` then extended the warning to every other
// output-producing command, and the allow list was not widened with it: ten
// stages went back to warning standalone and staying silent through pipe,
// which is the divergence `b59fc758` existed to close. An allow list has to be
// edited every time the standalone set grows; a deny list is right by default.
//
// decrypt is the only member: producing an unprotected document is exactly what
// it was asked to do, so [runSimpleMutation] opts it out via
// [withoutEncryptionWarning] and pipe must match. encrypt needs no entry, since
// it sets encryptsOutput and [warnEncryptionDroppedAddingEncryptStage] returns
// early when the output carries protection.
var silentStages = map[string]bool{
	"decrypt": true,
}

// xfaDroppingStages names the stages that discard an input's /XFA form data.
//
// An ALLOW list where [silentStages] is a deny list, and the inversion is
// measured rather than stylistic. Every stage drops encryption, so naming the
// exceptions is right there. Only split and merge drop XFA: rotate, insert,
// delete and autotag were each measured keeping /XFA through a rewrite, so a
// deny list here would warn about form data that is still in the output.
//
// remediate is absent deliberately. It removes /XFA because ISO 19005-2/3,
// 6.4.2 forbids the key in the profile the caller ASKED for, and its fixer
// reports "removed /XFA from AcroForm" in the standalone report, which makes it
// the decrypt case rather than a silent loss. flatten-form is absent for the
// reason its standalone command is: removing the form is the point of the verb.
var xfaDroppingStages = map[string]bool{
	"split": true, "merge": true,
}

// addStage parses stage-specific flags and appends the stage to the pipeline.
//
// It returns the cleanup functions of every document it opened. They belong to
// the caller, not to this function: [pdf.Pipeline.Merge] keeps the readers it
// is given (it copies the slice, not the documents) and the stage reads them
// when [pdf.Pipeline.WriteTo] executes, so closing them here would be a
// use-after-close. Release them after the write, as the standalone merge
// command does with its own.
//
// stderr receives each stage FlagSet's own output. A FlagSet defaults to the
// process's os.Stderr, which contradicts [run]'s contract that every byte goes
// to the writers the caller passed: a stage's flag error printed its message
// and usage past any capture the caller had installed, while only the wrapping
// "pipe stage" line reached the writer. Every other command in this package
// wires its FlagSet the same way.
//
// An autotag stage appends its report to autoTagged when the stage runs. The
// Writer fills it later, when it applies the edit, so the caller reads it only
// after [pdf.Pipeline.WriteTo] returns.
func addStage(ctx context.Context, p *pdf.Pipeline, src *pdf.Reader, name string, args []string, password string, stderr io.Writer, residual *[]error, opened *[]*pdf.Reader, autoTagged *[]*pdf.AutoTagReport) ([]func(), error) {
	switch name {
	case "rotate":
		fs := flag.NewFlagSet("rotate", flag.ContinueOnError)
		fs.SetOutput(stderr)
		angle := fs.Int("angle", 0, "Rotation angle (multiple of 90)")
		pages := fs.String("pages", "", "Page range")
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		p.Apply(func(ctx context.Context, w *pdf.Writer) error {
			pageCount, _ := src.PageCount(ctx)
			var pageNums []int
			if *pages != "" {
				var err error
				pageNums, err = parsePages(*pages, pageCount)
				if err != nil {
					return err
				}
			}
			return w.RotatePages(*angle, pageNums...)
		})

	case "split":
		fs := flag.NewFlagSet("split", flag.ContinueOnError)
		fs.SetOutput(stderr)
		pages := fs.String("pages", "", "Page range (required)")
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if *pages == "" {
			return nil, fmt.Errorf("--pages is required")
		}
		pageCount, _ := src.PageCount(ctx)
		pageNums, err := parsePages(*pages, pageCount)
		if err != nil {
			return nil, err
		}
		p.Split(pageNums)

	case "merge":
		fs := flag.NewFlagSet("merge", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		mergeFiles := fs.Args()
		if len(mergeFiles) == 0 {
			return nil, fmt.Errorf("merge requires at least one file argument")
		}
		var others []*pdf.Reader
		var closers []func()
		for _, f := range mergeFiles {
			r, closer, err := openPDF(ctx, f, password)
			if err != nil {
				// Release what this stage already opened before giving up, so a
				// bad source among several does not strand the good ones.
				for _, c := range closers {
					c()
				}
				return nil, fmt.Errorf("opening merge source %q: %w", f, err)
			}
			others = append(others, r)
			closers = append(closers, closer)
		}
		if opened != nil {
			*opened = append(*opened, others...)
		}
		p.Merge(others)
		return closers, nil

	case "delete":
		fs := flag.NewFlagSet("delete", flag.ContinueOnError)
		fs.SetOutput(stderr)
		pages := fs.String("pages", "", "Page range (required)")
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if *pages == "" {
			return nil, fmt.Errorf("--pages is required")
		}
		p.Apply(func(ctx context.Context, w *pdf.Writer) error {
			pageCount, _ := src.PageCount(ctx)
			pageNums, err := parsePages(*pages, pageCount)
			if err != nil {
				return err
			}
			return w.DeletePages(pageNums...)
		})

	case "encrypt":
		fs := flag.NewFlagSet("encrypt", flag.ContinueOnError)
		fs.SetOutput(stderr)
		userPw := fs.String("user-password", "", "User password")
		ownerPw := fs.String("owner-password", "", "Owner password")
		algo := fs.String("algorithm", "aes256", "Algorithm: rc4-128, aes128, aes256")
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		p.Apply(func(_ context.Context, w *pdf.Writer) error {
			a, ok := encryptAlgorithms[*algo]
			if !ok {
				return fmt.Errorf("unknown algorithm %q (valid: rc4-128, aes128, aes256)", *algo)
			}
			w.Encrypt(pdf.EncryptConfig{
				UserPassword:  *userPw,
				OwnerPassword: *ownerPw,
				Algorithm:     a,
				Permissions:   allPermissions,
			})
			return nil
		})

	case "decrypt":
		p.Apply(func(_ context.Context, w *pdf.Writer) error {
			w.RemoveEncryption()
			return nil
		})

	case "set-metadata":
		fs := flag.NewFlagSet("set-metadata", flag.ContinueOnError)
		fs.SetOutput(stderr)
		title := fs.String("title", "", "Title")
		author := fs.String("author", "", "Author")
		subject := fs.String("subject", "", "Subject")
		keywords := fs.String("keywords", "", "Keywords")
		creator := fs.String("creator", "", "Creator")
		producer := fs.String("producer", "", "Producer")
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		p.Apply(func(_ context.Context, w *pdf.Writer) error {
			w.SetMetadata(pdf.Metadata{
				Title:    *title,
				Author:   *author,
				Subject:  *subject,
				Keywords: *keywords,
				Creator:  *creator,
				Producer: *producer,
			})
			return nil
		})

	case "set-form":
		fs := flag.NewFlagSet("set-form", flag.ContinueOnError)
		fs.SetOutput(stderr)
		var fields fieldFlags
		fs.Var(&fields, "field", "Field name=value (repeatable)")
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		p.Apply(func(_ context.Context, w *pdf.Writer) error {
			for _, kv := range fields {
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid field format %q (expected name=value)", kv)
				}
				w.SetFormField(parts[0], parts[1])
			}
			return nil
		})

	case "flatten-form":
		p.Apply(func(_ context.Context, w *pdf.Writer) error {
			w.FlattenForm()
			return nil
		})

	case "stamp-text":
		fs := flag.NewFlagSet("stamp-text", flag.ContinueOnError)
		fs.SetOutput(stderr)
		text := fs.String("text", "", "Stamp text (required)")
		opacity := fs.Float64("opacity", 1.0, "Opacity 0.0-1.0")
		rotation := fs.Float64("rotation", 0, "Rotation degrees")
		color := fs.String("color", "", "Hex color (#rrggbb)")
		// The stage lacked this flag while the shared stamp path tells a caller
		// to "supply a TTF with -font" when WinAnsi cannot express the text, so
		// a pipeline was handed advice its own surface then rejected with
		// "flag provided but not defined". It is also the only way to stamp and
		// keep PDF/A conformance: the default standard font is not embedded,
		// which ISO 19005-2, 6.2.11.4.1 forbids.
		fontPath := fs.String("font", "", "Path to a TTF font file")
		fontSize := fs.Float64("font-size", 36, "Font size")
		x := fs.Float64("x", 0, "X position")
		y := fs.Float64("y", 0, "Y position")
		pages := fs.String("pages", "", "Page range")
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if *text == "" {
			return nil, fmt.Errorf("--text is required")
		}
		p.Apply(func(ctx context.Context, w *pdf.Writer) error {
			stamp := pdf.TextStamp{
				Text:     *text,
				FontSize: *fontSize,
				X:        *x,
				Y:        *y,
				Opacity:  *opacity,
				Rotation: *rotation,
			}
			if *color != "" {
				c, err := parseHexColor(*color)
				if err != nil {
					return err
				}
				stamp.Color = c
			}
			// Loaded here rather than at stage-build time to match the color
			// branch above: this stage reports argument problems from inside
			// the closure, and the standalone command's earlier load is its own
			// convention, not one worth splitting this switch over.
			if *fontPath != "" {
				data, err := os.ReadFile(*fontPath) //nolint:gosec // path comes from the -font CLI flag, not untrusted input
				if err != nil {
					return fmt.Errorf("reading font file: %w", err)
				}
				f, err := pdf.NewTTFFont(data)
				if err != nil {
					return fmt.Errorf("loading font: %w", err)
				}
				stamp.Font = f
			}
			var pageNums []int
			if *pages != "" {
				pageCount, _ := src.PageCount(ctx)
				var err error
				pageNums, err = parsePages(*pages, pageCount)
				if err != nil {
					return err
				}
			}
			return w.StampText(stamp, pageNums...)
		})

	case "stamp-image":
		fs := flag.NewFlagSet("stamp-image", flag.ContinueOnError)
		fs.SetOutput(stderr)
		image := fs.String("image", "", "Image file path (required)")
		mime := fs.String("mime", "", "MIME type (auto-detected if empty)")
		opacity := fs.Float64("opacity", 1.0, "Opacity")
		x := fs.Float64("x", 0, "X position")
		y := fs.Float64("y", 0, "Y position")
		width := fs.Float64("width", 0, "Width")
		height := fs.Float64("height", 0, "Height")
		pages := fs.String("pages", "", "Page range")
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if *image == "" {
			return nil, fmt.Errorf("--image is required")
		}
		imgData, err := os.ReadFile(*image)
		if err != nil {
			return nil, fmt.Errorf("reading image: %w", err)
		}
		mimeType := *mime
		if mimeType == "" {
			mimeType = mimeFromExt(*image)
		}
		p.Apply(func(ctx context.Context, w *pdf.Writer) error {
			stamp := pdf.ImageStamp{
				Data:     imgData,
				MimeType: mimeType,
				X:        *x,
				Y:        *y,
				Width:    *width,
				Height:   *height,
				Opacity:  *opacity,
			}
			var pageNums []int
			if *pages != "" {
				pageCount, _ := src.PageCount(ctx)
				var err error
				pageNums, err = parsePages(*pages, pageCount)
				if err != nil {
					return err
				}
			}
			return w.StampImage(stamp, pageNums...)
		})

	case "remediate":
		fs := flag.NewFlagSet("remediate", flag.ContinueOnError)
		fs.SetOutput(stderr)
		profile := fs.String("profile", "", "Compliance profile (required)")
		aggressive := fs.Bool("aggressive", false, "Enable aggressive fixers")
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if *profile == "" {
			return nil, fmt.Errorf("--profile is required")
		}
		p.Apply(func(ctx context.Context, w *pdf.Writer) error {
			var opts []pdf.RemediateOption
			if *aggressive {
				opts = append(opts, pdf.WithAggressiveFixes())
			}
			// The report was discarded here because this command prints
			// nothing. That answered the PRINTING question only: the exit code
			// is a separate channel, and --strict needs the residuals to judge.
			if pdfaP, ok := pdfaProfiles[*profile]; ok {
				rep, err := w.RemediatePDFA(ctx, pdfaP, opts...)
				if rep != nil && residual != nil {
					*residual = append(*residual, rep.Residual...)
				}
				return err
			}
			if pdfuaP, ok := pdfuaProfiles[*profile]; ok {
				rep, err := w.RemediatePDFUA(ctx, pdfuaP, opts...)
				if rep != nil && residual != nil {
					*residual = append(*residual, rep.Residual...)
				}
				return err
			}
			// WTPDF reaches the standalone command but never reached here.
			// The commit that wired the profile into the CLI touched
			// remediate.go and validate.go and left this stage alone, so
			// `pipe ... remediate -profile WTPDF-1.0-Reuse` answered "unknown
			// profile" for a level litho remediates perfectly well.
			//
			// Factur-X stays out on purpose rather than by oversight: it needs
			// the invoice XML and conformance level that this stage has no
			// flags to accept, and inventing them here would be a feature, not
			// a wiring fix. The error names the profile and claims nothing
			// about what else is available, so it stays honest either way.
			if wtpdfP, ok := wtpdfProfiles[*profile]; ok {
				rep, err := w.RemediateWTPDF(ctx, wtpdfP, opts...)
				if rep != nil && residual != nil {
					*residual = append(*residual, rep.Residual...)
				}
				return err
			}
			return fmt.Errorf("unknown profile %q", *profile)
		})

	case "attach":
		fs := flag.NewFlagSet("attach", flag.ContinueOnError)
		fs.SetOutput(stderr)
		name := fs.String("name", "", "Attachment name as stored in the PDF (required)")
		dataPath := fs.String("data", "", "Path to the file to attach (required)")
		mime := fs.String("mime", "", "MIME type (auto-detected from extension if omitted)")
		relationship := fs.String("relationship", "", "AFRelationship for PDF/A-3+ associated files (Source, Data, Alternative, Supplement, EncryptedPayload, FormData, Schema, Unspecified)")
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if *name == "" || *dataPath == "" {
			return nil, fmt.Errorf("--name and --data are required")
		}
		data, err := os.ReadFile(*dataPath)
		if err != nil {
			return nil, fmt.Errorf("reading attachment file: %w", err)
		}
		mimeType := *mime
		if mimeType == "" {
			mimeType = mimeFromExt(*dataPath)
		}
		p.Apply(func(_ context.Context, w *pdf.Writer) error {
			// The standalone command's two writer calls, kept apart for the
			// same reason: an associated file carries an /AFRelationship that
			// PDF/A-3 and Factur-X require, and a plain attachment has none.
			if *relationship != "" {
				w.AddAssociatedFile(pdf.AssociatedFile{
					Name:         *name,
					Data:         data,
					MimeType:     mimeType,
					Relationship: *relationship,
				})
				return nil
			}
			w.AddAttachment(*name, data, mimeType)
			return nil
		})

	case "detach":
		fs := flag.NewFlagSet("detach", flag.ContinueOnError)
		fs.SetOutput(stderr)
		name := fs.String("name", "", "Attachment name to remove (required)")
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if *name == "" {
			return nil, fmt.Errorf("--name is required")
		}
		p.Apply(func(_ context.Context, w *pdf.Writer) error {
			w.RemoveAttachment(*name)
			return nil
		})

	case "insert":
		fs := flag.NewFlagSet("insert", flag.ContinueOnError)
		fs.SetOutput(stderr)
		source := fs.String("source", "", "Source PDF file (required)")
		pos := fs.Int("pos", -1, "0-based insertion position (required)")
		sourcePages := fs.String("source-pages", "", "Source page range (default: all)")
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if *source == "" || *pos < 0 {
			return nil, fmt.Errorf("--source and a --pos of 0 or more are required")
		}
		// Opened with the global password, as the standalone command opens its
		// source with the same -p it opened the target with.
		srcReader, srcCloser, err := openPDF(ctx, *source, password)
		if err != nil {
			return nil, fmt.Errorf("opening insert source %q: %w", *source, err)
		}
		p.Apply(func(ctx context.Context, w *pdf.Writer) error {
			var srcPageList []int
			if *sourcePages != "" {
				srcPageCount, err := srcReader.PageCount(ctx)
				if err != nil {
					return fmt.Errorf("reading source page count: %w", err)
				}
				srcPageList, err = parsePages(*sourcePages, srcPageCount)
				if err != nil {
					return err
				}
			}
			return w.InsertPages(*pos, srcReader, srcPageList...)
		})
		if opened != nil {
			*opened = append(*opened, srcReader)
		}
		// The reader stays live until WriteTo executes this stage, so the
		// closer belongs to the caller. See this function's doc comment.
		return []func(){srcCloser}, nil

	case "set-bookmarks":
		fs := flag.NewFlagSet("set-bookmarks", flag.ContinueOnError)
		fs.SetOutput(stderr)
		fromJSON := fs.String("from-json", "", "JSON file with bookmark definitions (required unless --generate)")
		generate := fs.Bool("generate", false, "Auto-generate bookmarks from detected headings (instead of --from-json)")
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if *fromJSON == "" && !*generate {
			return nil, fmt.Errorf("--from-json or --generate is required")
		}
		// Read and parsed before the pipeline runs: a malformed JSON file is an
		// argument error, and reporting it here names the stage rather than
		// surfacing as a mid-write failure that abandons the output.
		var bookmarks []models.Bookmark
		if !*generate {
			data, err := os.ReadFile(*fromJSON)
			if err != nil {
				return nil, fmt.Errorf("reading bookmark file: %w", err)
			}
			if err := json.Unmarshal(data, &bookmarks); err != nil {
				return nil, fmt.Errorf("parsing bookmark JSON: %w", err)
			}
		}
		p.Apply(func(_ context.Context, w *pdf.Writer) error {
			if *generate {
				w.GenerateBookmarks(defaultBookmarkGenOptions(), pdf.BookmarkReplace)
				return nil
			}
			w.SetBookmarks(bookmarks)
			return nil
		})

	case "stamp-pdf":
		fs := flag.NewFlagSet("stamp-pdf", flag.ContinueOnError)
		fs.SetOutput(stderr)
		stampPath := fs.String("stamp", "", "Path to stamp PDF file (required)")
		stampPage := fs.Int("stamp-page", 1, "1-based page number in the stamp PDF")
		x := fs.Float64("x", 0, "X position in points from bottom-left")
		y := fs.Float64("y", 0, "Y position in points from bottom-left")
		pages := fs.String("pages", "", "Page range (default: all)")
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if *stampPath == "" {
			return nil, fmt.Errorf("--stamp is required")
		}
		// The global password, matching the standalone command and the insert
		// stage above. Opening the overlay with no password made an encrypted
		// stamp unreachable from either surface.
		stampReader, stampCloser, err := openPDF(ctx, *stampPath, password)
		if err != nil {
			return nil, fmt.Errorf("opening stamp PDF: %w", err)
		}
		p.Apply(func(ctx context.Context, w *pdf.Writer) error {
			var pageNums []int
			if *pages != "" {
				pageCount, err := src.PageCount(ctx)
				if err != nil {
					return fmt.Errorf("reading page count: %w", err)
				}
				pageNums, err = parsePages(*pages, pageCount)
				if err != nil {
					return err
				}
			}
			return w.StampPDF(pdf.PDFStamp{
				Source: stampReader,
				Page:   *stampPage,
				X:      *x,
				Y:      *y,
			}, pageNums...)
		})
		if opened != nil {
			*opened = append(*opened, stampReader)
		}
		// The reader stays live until WriteTo executes this stage, so the
		// closer belongs to the caller. See this function's doc comment.
		return []func(){stampCloser}, nil

	case "autotag":
		p.Apply(func(_ context.Context, w *pdf.Writer) error {
			*autoTagged = append(*autoTagged, w.AutoTag())
			return nil
		})

	default:
		return nil, fmt.Errorf("unknown stage %q", name)
	}

	return nil, nil
}
