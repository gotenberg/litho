package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// TestUsageListsEveryDispatchableCommand keeps the top-level help honest, the
// rule [TestLogLevel_IsAdvertised] states for a flag and
// [TestPipeUsage_AdvertisesEveryStage] states for a pipe stage.
//
// Both sides are hand-written and neither knows about the other: printUsage is
// a block of Fprintln calls, runCommand is a switch. A command added to one and
// not the other fails in a way nothing reports. Unlisted, it is undiscoverable,
// since the usage block is the only place litho enumerates its commands.
// Listed but undispatchable, it answers "unknown command" to a caller who read
// the help, which is the defect `e7222fc8` fixed for `litho sign` in README.
//
// Measured when this test was written: 39 listed, 39 dispatchable, no
// asymmetry. This pins that rather than establishing it.
//
// The usage rows are parsed by splitting on whitespace runs, NOT by requiring
// two spaces before the description. The block aligns descriptions at a fixed
// column, so the longest name (set-bookmarks) is separated by a single space,
// and a `\s{2,}` pattern silently drops exactly that row.
func TestUsageListsEveryDispatchableCommand(t *testing.T) {
	t.Parallel()

	dispatched := dispatchCommandNames(t)
	listed := usageCommandNames(t)

	// Controls: a parser that read nothing would report perfect agreement.
	if len(dispatched) < 30 {
		t.Fatalf("found only %d dispatch cases, so the switch was not parsed", len(dispatched))
	}
	if len(listed) < 30 {
		t.Fatalf("found only %d commands in the usage block, so it was not parsed", len(listed))
	}

	for name := range dispatched {
		if !listed[name] {
			t.Errorf("%q is dispatchable but the usage block never lists it, so nobody reading "+
				"litho's help can discover it", name)
		}
	}
	for name := range listed {
		if !dispatched[name] {
			t.Errorf("the usage block advertises %q but runCommand has no case for it, so it "+
				"answers \"unknown command\" to a caller who read the help", name)
		}
	}
}

// dispatchCommandNames returns every command runCommand switches on.
//
// Read with go/ast rather than grepped: a case clause may carry several
// literals, and the question is which strings the switch matches, which a text
// pattern answers only by accident.
func dispatchCommandNames(t *testing.T) map[string]bool {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing main.go: %v", err)
	}

	names := make(map[string]bool)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "runCommand" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			clause, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range clause.List {
				lit, ok := expr.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				if name, err := strconv.Unquote(lit.Value); err == nil {
					names[name] = true
				}
			}
			return true
		})
	}
	return names
}

// usageCommandNames returns the command names printUsage advertises, taken
// from the writer it is handed rather than from a rebuilt copy of the text.
func usageCommandNames(t *testing.T) map[string]bool {
	t.Helper()

	var buf bytes.Buffer
	printUsage(&buf)

	names := make(map[string]bool)
	for line := range strings.SplitSeq(buf.String(), "\n") {
		// Command rows are indented and start with the name. Section headers
		// are flush left, and the global flag rows start with a dash.
		if !strings.HasPrefix(line, "  ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		if !isCommandToken(name) {
			continue
		}
		names[name] = true
	}
	return names
}

// isCommandToken reports whether a usage token looks like a command name:
// lowercase letters, digits and dashes, never a flag.
func isCommandToken(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}
