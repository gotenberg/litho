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

// internalSubcommands are the subcommands other processes run, which the
// usage leaves out: the provisioning image runs provision and scan-claims,
// the robustness feature convert-one.
var internalSubcommands = []string{"provision", "scan-claims", "convert-one"}

// helpAliases are dispatched but are not subcommands, so neither list below
// has to name them.
var helpAliases = map[string]bool{"-h": true, "--help": true, "help": true}

// TestUsageNamesEveryDispatchableSubcommand keeps the usage block honest, the
// rule cmd/litho's TestUsageListsEveryDispatchableCommand states for litho
// itself: the "Subcommands:" block and the dispatch switch are both
// hand-written, so each is checked against the other. A subcommand the usage
// never names is one nobody finds, and a name the usage invents sends a
// reader to "unknown subcommand". The internal subcommands, which other
// processes run, are dispatched and left out of the usage.
func TestUsageNamesEveryDispatchableSubcommand(t *testing.T) {
	dispatched := dispatchSubcommandNames(t)

	// Control: a parser that read nothing would report perfect agreement.
	if len(dispatched) < 4 {
		t.Fatalf("found only %d dispatch cases, so the switch was not parsed: %v", len(dispatched), dispatched)
	}
	for _, want := range []string{"scan-claims", "prepare"} {
		if !dispatched[want] {
			t.Fatalf("%q missing from the parsed dispatch set, so the comparison means nothing", want)
		}
	}
	internal := map[string]bool{}
	for _, name := range internalSubcommands {
		if !dispatched[name] {
			t.Errorf("internalSubcommands lists %q, which dispatch has no case for", name)
		}
		internal[name] = true
	}

	var buf bytes.Buffer
	printUsage(&buf)
	block := usageBlockNames(t, buf.String())
	for name := range dispatched {
		if !internal[name] && !block[name] {
			t.Errorf("%q is dispatchable but the Subcommands block never names it, so a reader cannot discover it", name)
		}
	}
	for name := range block {
		if !dispatched[name] {
			t.Errorf("the Subcommands block advertises %q but dispatch has no case for it", name)
		}
		if internal[name] {
			t.Errorf("the Subcommands block advertises the internal %q", name)
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
