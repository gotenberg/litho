package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// limitFlag is a global flag that sets one budget of [pdf.Limits].
type limitFlag struct {
	// name is the flag without its leading dashes.
	name string
	// usage describes the budget in the help.
	usage string
	// size marks a byte count, which takes a binary suffix.
	size bool
	// set stores value, parsed, in l.
	set func(l *pdf.Limits, value string) error
	// get reads the budget from l, for the defaults the help shows.
	get func(l pdf.Limits) int64
}

// limitFlags lists the resource-limit flags in the order the help shows them.
// An immutable lookup table, like the other package-level tables here.
var limitFlags = []limitFlag{
	{
		name: "max-stream-bytes", usage: "Decoded bytes of one stream", size: true,
		set: func(l *pdf.Limits, v string) error {
			n, err := parseByteSize(v)
			l.MaxStreamBytes = n
			return err
		},
		get: func(l pdf.Limits) int64 { return l.MaxStreamBytes },
	},
	{
		name: "max-image-pixels", usage: "Pixels of one image",
		set: func(l *pdf.Limits, v string) error {
			n, err := parseCount64(v)
			l.MaxImagePixels = n
			return err
		},
		get: func(l pdf.Limits) int64 { return l.MaxImagePixels },
	},
	{
		name: "max-embedded-file-bytes", usage: "Size of an embedded PDF validation opens", size: true,
		set: func(l *pdf.Limits, v string) error {
			n, err := parseByteSize(v)
			l.MaxEmbeddedFileBytes = n
			return err
		},
		get: func(l pdf.Limits) int64 { return l.MaxEmbeddedFileBytes },
	},
	{
		name: "max-form-depth", usage: "Form XObjects nested below a page",
		set: func(l *pdf.Limits, v string) error {
			n, err := parseCount(v)
			l.MaxFormDepth = n
			return err
		},
		get: func(l pdf.Limits) int64 { return int64(l.MaxFormDepth) },
	},
	{
		name: "max-cached-objects", usage: "Parsed objects kept in memory",
		set: func(l *pdf.Limits, v string) error {
			n, err := parseCount(v)
			l.MaxCachedObjects = n
			return err
		},
		get: func(l pdf.Limits) int64 { return int64(l.MaxCachedObjects) },
	},
}

// limitFlagFor returns the resource-limit flag arg names, in its space form
// ("--max-form-depth") or its equals form ("--max-form-depth=20"), or nil.
func limitFlagFor(arg string) *limitFlag {
	rest, ok := strings.CutPrefix(arg, "--")
	if !ok {
		return nil
	}
	name, _, _ := strings.Cut(rest, "=")
	for i := range limitFlags {
		if limitFlags[i].name == name {
			return &limitFlags[i]
		}
	}
	return nil
}

// errNegative is the reason a budget below zero is refused.
var errNegative = errors.New("a limit must not be negative")

// byteUnits are the binary suffixes a byte count takes, largest first.
var byteUnits = []struct {
	suffix string
	bytes  int64
}{
	{"GiB", 1 << 30},
	{"MiB", 1 << 20},
	{"KiB", 1 << 10},
}

// parseByteSize reads a byte count: a plain integer, or one followed by KiB,
// MiB or GiB.
func parseByteSize(value string) (int64, error) {
	number, mult := value, int64(1)
	for _, u := range byteUnits {
		if n, ok := strings.CutSuffix(value, u.suffix); ok {
			number, mult = n, u.bytes
			break
		}
	}
	n, err := parseCount64(number)
	if err != nil {
		return 0, err
	}
	if n > math.MaxInt64/mult {
		return 0, fmt.Errorf("%s overflows a 64-bit byte count", value)
	}
	return n * mult, nil
}

// parseCount64 reads a plain non-negative integer.
func parseCount64(value string) (int64, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("not an integer: %w", err)
	}
	if n < 0 {
		return 0, errNegative
	}
	return n, nil
}

// parseCount reads a plain non-negative integer that fits an int.
func parseCount(value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("not an integer: %w", err)
	}
	if n < 0 {
		return 0, errNegative
	}
	return n, nil
}

// formatByteSize writes n in the largest binary unit that holds it exactly.
func formatByteSize(n int64) string {
	for _, u := range byteUnits {
		if n >= u.bytes && n%u.bytes == 0 {
			return strconv.FormatInt(n/u.bytes, 10) + u.suffix
		}
	}
	return strconv.FormatInt(n, 10)
}

// limitExample returns a value of f for the message that asks for one: twice
// its default, written as the help writes it.
func limitExample(f *limitFlag) string {
	twice := 2 * f.get(pdf.DefaultLimits())
	if f.size {
		return formatByteSize(twice)
	}
	return strconv.FormatInt(twice, 10)
}

// printLimitUsage writes the resource-limit flags to the help, with the
// defaults the library applies.
func printLimitUsage(w io.Writer) {
	defaults := pdf.DefaultLimits()
	fmt.Fprintln(w, "Resource limits (global flags; 0 keeps the default, sizes take KiB, MiB or GiB):")
	for _, f := range limitFlags {
		def := strconv.FormatInt(f.get(defaults), 10)
		if f.size {
			def = formatByteSize(f.get(defaults))
		}
		fmt.Fprintf(w, "  --%-27s %s (default %s)\n", f.name+" N", f.usage, def)
	}
}

// cliLimitsKey addresses the budgets the resource-limit flags install on the
// context.
type cliLimitsKey struct{}

// withCLILimits returns ctx carrying the budgets the resource-limit flags set,
// for [openOptions] to hand the library.
func withCLILimits(ctx context.Context, l pdf.Limits) context.Context {
	return context.WithValue(ctx, cliLimitsKey{}, l)
}

// cliLimitsFrom returns the budgets the resource-limit flags set, and whether
// any flag was given. When none was, the library keeps its defaults.
func cliLimitsFrom(ctx context.Context) (pdf.Limits, bool) {
	l, ok := ctx.Value(cliLimitsKey{}).(pdf.Limits)
	return l, ok
}
