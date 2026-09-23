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

// helpAliases are dispatched but are not subcommands, so neither list below
// has to name them.
var helpAliases = map[string]bool{"-h": true, "--help": true, "help": true}

// TestUsageNamesEveryDispatchableSubcommand keeps the usage block honest, the
// rule cmd/litho's TestUsageListsEveryDispatchableCommand states for litho
// itself and TestPipeUsage_AdvertisesEveryStage states for a pipe stage.
//
// This tool prints its subcommands TWICE: a one-line synopsis and a
// "Subcommands:" block under it. Both are hand-written, the dispatch switch is
// a third hand-written list, and none knew about the others. Measured when this
// test was written, the synopsis read `<init|run|baseline|check>` while the
// block beneath it and the switch both carried five, so the tool contradicted
// itself two lines apart. `scan-claims` is the one that went missing, and it is
// the subcommand the provisioning Docker entrypoint actually invokes.
//
// Both directions are checked for both lists: a subcommand the usage never
// names is one nobody finds, and a name the usage invents sends a reader to
// "unknown subcommand" for a surface this tool advertises.
func TestUsageNamesEveryDispatchableSubcommand(t *testing.T) {
	dispatched := dispatchSubcommandNames(t)

	// Control: a parser that read nothing would report perfect agreement.
	if len(dispatched) < 4 {
		t.Fatalf("found only %d dispatch cases, so the switch was not parsed: %v", len(dispatched), dispatched)
	}
	for _, want := range []string{"scan-claims", "init"} {
		if !dispatched[want] {
			t.Fatalf("%q missing from the parsed dispatch set, so the comparison means nothing", want)
		}
	}

	var buf bytes.Buffer
	printUsage(&buf)
	synopsis := usageSynopsisNames(t, buf.String())
	block := usageBlockNames(t, buf.String())

	for _, tc := range []struct {
		desc  string
		named map[string]bool
	}{
		{"the one-line synopsis", synopsis},
		{"the Subcommands block", block},
	} {
		for name := range dispatched {
			if !tc.named[name] {
				t.Errorf("%q is dispatchable but %s never names it, so a reader cannot discover it",
					name, tc.desc)
			}
		}
		for name := range tc.named {
			if !dispatched[name] {
				t.Errorf("%s advertises %q but dispatch has no case for it, so it answers "+
					"\"unknown subcommand\" to a reader who followed the help", tc.desc, name)
			}
		}
	}
}

// dispatchSubcommandNames returns every subcommand dispatch switches on, minus
// the help aliases.
//
// Read with go/ast rather than grepped: a case clause carries several literals
// (the help arm has three), and the question is which strings the switch
// matches, which a text pattern answers only by accident.
func dispatchSubcommandNames(t *testing.T) map[string]bool {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing main.go: %v", err)
	}

	names := make(map[string]bool)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "dispatch" {
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
				name, unquoteErr := strconv.Unquote(lit.Value)
				if unquoteErr != nil || helpAliases[name] {
					continue
				}
				names[name] = true
			}
			return true
		})
	}
	return names
}

// usageSynopsisNames returns the names inside the "Usage: corpus <...>" group.
func usageSynopsisNames(t *testing.T, usage string) map[string]bool {
	t.Helper()

	names := make(map[string]bool)
	for line := range strings.SplitSeq(usage, "\n") {
		if !strings.HasPrefix(line, "Usage: corpus ") {
			continue
		}
		open := strings.Index(line, "<")
		close := strings.Index(line, ">")
		if open < 0 || close < open {
			t.Fatalf("synopsis %q carries no <a|b> group", line)
		}
		for _, name := range strings.Split(line[open+1:close], "|") {
			if name = strings.TrimSpace(name); name != "" {
				names[name] = true
			}
		}
		return names
	}
	t.Fatalf("no synopsis line in the usage block:\n%s", usage)
	return nil
}

// usageBlockNames returns the names the "Subcommands:" block lists, reading the
// indented rows under it and stopping at the blank line that ends the section
// so the flag rows below are not mistaken for subcommands.
func usageBlockNames(t *testing.T, usage string) map[string]bool {
	t.Helper()

	names := make(map[string]bool)
	inBlock := false
	for line := range strings.SplitSeq(usage, "\n") {
		if strings.HasPrefix(line, "Subcommands:") {
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		if !strings.HasPrefix(line, "  ") {
			break
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			names[fields[0]] = true
		}
	}
	if len(names) == 0 {
		t.Fatalf("parsed no rows out of the Subcommands block:\n%s", usage)
	}
	return names
}
