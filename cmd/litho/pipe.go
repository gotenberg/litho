package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

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
	fmt.Fprintln(w, "Stages: rotate, split, merge, delete, encrypt, decrypt, autotag,")
	fmt.Fprintln(w, "        set-metadata, set-form, flatten-form, stamp-text,")
	fmt.Fprintln(w, "        stamp-image, remediate")
}

// runPipe implements `litho pipe <file> <stage> [flags] <stage> [flags] ... -o out.pdf`.
// Stages reuse the same names and flags as individual mutation commands.
func runPipe(ctx context.Context, args []string, _stdout, stderr io.Writer) int {
	// pipe owns no FlagSet, so the [flag.ErrHelp] path the other commands take
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
	if outputFile == "" {
		fmt.Fprintln(stderr, "pipe: -o output file is required")
		return exitError
	}

	r, closer, err := openPDF(ctx, inputFile, password)
	if err != nil {
		fmt.Fprintf(stderr, "pipe: %v\n", err)
		return exitError
	}
	defer closer()

	pipeline := pdf.NewPipeline().Open(r)

	// Split stageArgs into groups: each group starts with a stage name.
	groups := splitStageGroups(stageArgs)
	if len(groups) == 0 {
		fmt.Fprintln(stderr, "pipe: no stages specified")
		return exitError
	}

	// An encrypt stage makes the final output encrypted, which the write
	// engine refuses to compact. splitStageGroups guarantees the first token
	// of a group is the stage name, so this reads the pipeline rather than
	// guessing at the argument list.
	encryptsOutput := false
	for _, g := range groups {
		if len(g) > 0 && g[0] == "encrypt" {
			encryptsOutput = true
			break
		}
	}
	warnCompactSkipped(stderr, compact, encryptsOutput)

	// Residual violations left by any remediate stage, judged after the write
	// when --strict was passed. Stages run sequentially, [pdf.Pipeline.WriteTo]
	// iterating its stage list with no goroutines, so a stage closure appending
	// here needs no synchronisation.
	var remediationResidual []error

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

		closers, err := addStage(ctx, pipeline, r, stageName, stageFlags, password, stderr, &remediationResidual)
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

	if err := pipeline.Validate(); err != nil {
		fmt.Fprintf(stderr, "pipe: %v\n", err)
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
// whole command with "flag needs an argument: -title". All thirteen names broke
// that way, while the --flag=value form was unaffected, which made it a
// confusing failure to hit.
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
// runs two stages, not one. -aggressive is the only boolean among them.
var boolStageFlags = map[string]bool{"aggressive": true}

// expectsValue reports whether arg is a stage flag that consumes the next
// token. The --flag=value form carries its own value and consumes nothing.
func expectsValue(arg string) bool {
	if !strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
		return false
	}
	return !boolStageFlags[strings.TrimLeft(arg, "-")]
}

var stageCmds = map[string]bool{
	"rotate": true, "split": true, "merge": true, "delete": true,
	"encrypt": true, "decrypt": true, "set-metadata": true,
	"set-form": true, "flatten-form": true, "stamp-text": true,
	"stamp-image": true, "remediate": true, "autotag": true,
}

func isStageCmd(s string) bool { return stageCmds[s] }

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
func addStage(ctx context.Context, p *pdf.Pipeline, src *pdf.Reader, name string, args []string, password string, stderr io.Writer, residual *[]error) ([]func(), error) {
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

	case "autotag":
		p.Apply(func(_ context.Context, w *pdf.Writer) error {
			w.AutoTag()
			return nil
		})

	default:
		return nil, fmt.Errorf("unknown stage %q", name)
	}

	return nil, nil
}
