package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// runPipe implements `litho pipe <file> <stage> [flags] <stage> [flags] ... -o out.pdf`.
// Stages reuse the same names and flags as individual mutation commands.
func runPipe(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		fmt.Fprintln(stderr, "Usage: litho pipe <file> <stage> [stage-flags] ... -o out.pdf")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Stages: rotate, split, merge, delete, encrypt, decrypt, autotag,")
		fmt.Fprintln(stderr, "        set-metadata, set-form, flatten-form, stamp-text,")
		fmt.Fprintln(stderr, "        stamp-image, remediate")
		return exitError
	}

	// Extract -o, -p, and input file before stage parsing.
	inputFile, password, outputFile, stageArgs := parsePipeGlobals(args)
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

	for _, group := range groups {
		stageName := group[0]
		stageFlags := group[1:]

		if err := addStage(ctx, pipeline, r, stageName, stageFlags, password); err != nil {
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

	result, err := pipeline.WriteTo(ctx, dst)
	if closeErr := closeDst(); closeErr != nil {
		fmt.Fprintf(stderr, "pipe: closing output: %v\n", closeErr)
		return exitError
	}
	if err != nil {
		fmt.Fprintf(stderr, "pipe: %v\n", err)
		return exitError
	}

	_ = result
	return exitOK
}

// parsePipeGlobals extracts input file, -p, -o from args, returning the
// remaining args as stage arguments.
func parsePipeGlobals(args []string) (inputFile, password, outputFile string, rest []string) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-o" && i+1 < len(args):
			outputFile = args[i+1]
			i++
		case args[i] == "-p" && i+1 < len(args):
			password = args[i+1]
			i++
		case inputFile == "" && !strings.HasPrefix(args[i], "-"):
			inputFile = args[i]
		default:
			rest = append(rest, args[i])
		}
	}
	return
}

// splitStageGroups splits args into groups where each group starts with
// a known stage name.
func splitStageGroups(args []string) [][]string {
	var groups [][]string
	var current []string

	for _, arg := range args {
		if isStageCmd(arg) {
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

var stageCmds = map[string]bool{
	"rotate": true, "split": true, "merge": true, "delete": true,
	"encrypt": true, "decrypt": true, "set-metadata": true,
	"set-form": true, "flatten-form": true, "stamp-text": true,
	"stamp-image": true, "remediate": true, "autotag": true,
}

func isStageCmd(s string) bool { return stageCmds[s] }

// addStage parses stage-specific flags and appends the stage to the pipeline.
func addStage(ctx context.Context, p *pdf.Pipeline, src *pdf.Reader, name string, args []string, password string) error {
	switch name {
	case "rotate":
		fs := flag.NewFlagSet("rotate", flag.ContinueOnError)
		angle := fs.Int("angle", 0, "Rotation angle (multiple of 90)")
		pages := fs.String("pages", "", "Page range")
		if err := fs.Parse(args); err != nil {
			return err
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
		pages := fs.String("pages", "", "Page range (required)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *pages == "" {
			return fmt.Errorf("--pages is required")
		}
		pageCount, _ := src.PageCount(ctx)
		pageNums, err := parsePages(*pages, pageCount)
		if err != nil {
			return err
		}
		p.Split(pageNums)

	case "merge":
		fs := flag.NewFlagSet("merge", flag.ContinueOnError)
		if err := fs.Parse(args); err != nil {
			return err
		}
		mergeFiles := fs.Args()
		if len(mergeFiles) == 0 {
			return fmt.Errorf("merge requires at least one file argument")
		}
		var others []*pdf.Reader
		for _, f := range mergeFiles {
			r, _, err := openPDF(ctx, f, password)
			if err != nil {
				return fmt.Errorf("opening merge source %q: %w", f, err)
			}
			others = append(others, r)
		}
		p.Merge(others)

	case "delete":
		fs := flag.NewFlagSet("delete", flag.ContinueOnError)
		pages := fs.String("pages", "", "Page range (required)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *pages == "" {
			return fmt.Errorf("--pages is required")
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
		userPw := fs.String("user-password", "", "User password")
		ownerPw := fs.String("owner-password", "", "Owner password")
		algo := fs.String("algorithm", "aes256", "Algorithm: rc4-128, aes128, aes256")
		if err := fs.Parse(args); err != nil {
			return err
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
		title := fs.String("title", "", "Title")
		author := fs.String("author", "", "Author")
		subject := fs.String("subject", "", "Subject")
		keywords := fs.String("keywords", "", "Keywords")
		creator := fs.String("creator", "", "Creator")
		producer := fs.String("producer", "", "Producer")
		if err := fs.Parse(args); err != nil {
			return err
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
		var fields fieldFlags
		fs.Var(&fields, "field", "Field name=value (repeatable)")
		if err := fs.Parse(args); err != nil {
			return err
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
		text := fs.String("text", "", "Stamp text (required)")
		opacity := fs.Float64("opacity", 1.0, "Opacity 0.0-1.0")
		rotation := fs.Float64("rotation", 0, "Rotation degrees")
		color := fs.String("color", "", "Hex color (#rrggbb)")
		fontSize := fs.Float64("font-size", 36, "Font size")
		x := fs.Float64("x", 0, "X position")
		y := fs.Float64("y", 0, "Y position")
		pages := fs.String("pages", "", "Page range")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *text == "" {
			return fmt.Errorf("--text is required")
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
		image := fs.String("image", "", "Image file path (required)")
		mime := fs.String("mime", "", "MIME type (auto-detected if empty)")
		opacity := fs.Float64("opacity", 1.0, "Opacity")
		x := fs.Float64("x", 0, "X position")
		y := fs.Float64("y", 0, "Y position")
		width := fs.Float64("width", 0, "Width")
		height := fs.Float64("height", 0, "Height")
		pages := fs.String("pages", "", "Page range")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *image == "" {
			return fmt.Errorf("--image is required")
		}
		imgData, err := os.ReadFile(*image)
		if err != nil {
			return fmt.Errorf("reading image: %w", err)
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
		profile := fs.String("profile", "", "Compliance profile (required)")
		aggressive := fs.Bool("aggressive", false, "Enable aggressive fixers")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *profile == "" {
			return fmt.Errorf("--profile is required")
		}
		p.Apply(func(ctx context.Context, w *pdf.Writer) error {
			var opts []pdf.RemediateOption
			if *aggressive {
				opts = append(opts, pdf.WithAggressiveFixes())
			}
			if pdfaP, ok := pdfaProfiles[*profile]; ok {
				_, err := w.RemediatePDFA(ctx, pdfaP, opts...)
				return err
			}
			if pdfuaP, ok := pdfuaProfiles[*profile]; ok {
				_, err := w.RemediatePDFUA(ctx, pdfuaP, opts...)
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
		return fmt.Errorf("unknown stage %q", name)
	}

	return nil
}
