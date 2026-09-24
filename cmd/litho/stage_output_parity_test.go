package main

import (
	"bytes"
	"context"
	"sort"
	"strings"
	"testing"
)

// nonStageOutputCmds names the commands that write a document but deliberately
// do not run as a pipe stage. [stageCmds] carries the full reasoning; this
// repeats only enough to say why each is here rather than missing.
//
// Listing them is what lets the test below treat every OTHER output-producing
// command as a defect. Without the set, the check could only be written the
// other way round, and an allow list read from the same map it is checking
// proves nothing.
var nonStageOutputCmds = map[string]string{
	"sign":   "a later stage rewrites the document and voids the signature, and pipe cannot guarantee sign runs last",
	"redact": "redact's value is its report, and pipe prints nothing but the document",
	"pipe":   "pipe is the pipeline itself, so it cannot be one of its own stages",
}

// TestEveryOutputProducingCommandIsAStageOrDeliberatelyExcluded closes the last
// direction nothing checked about [stageCmds].
//
// Every existing guard runs OUTWARD from that map: [TestPipeUsage_AdvertisesEveryStage]
// compares it to the help text, [TestPipe_EveryBooleanStageFlagIsRegistered]
// compares it to boolStageFlags. Nothing ran INWARD, from the set of commands
// that actually write a document back to the map, and that is the direction an
// allow list drifts in: stageCmds has to be edited by hand every time litho
// grows a command with an -o, and forgetting costs nothing at build time.
//
// It is not hypothetical. pipe_output_stages_cli_test.go records the same
// omission happening five at a time: attach, detach, insert, set-bookmarks and
// stamp-pdf were each "an ordinary mutation command with an -o" that answered
// `unknown stage` while pipe's own doc comment claimed "Stages reuse the same
// names and flags as individual mutation commands". They were added by hand;
// nothing stopped the sixth.
//
// The same reasoning drove `b59fc758`, which inverted silentStages from an
// allow list to a deny list because "an allow list has to be edited every time
// the standalone set grows; a deny list is right by default". stageCmds cannot
// be inverted, since most commands are read-only reports that should never be
// stages, so it gets a check instead.
//
// Output-producing is decided by RUNNING each command's help and looking for an
// -o flag, not by reading the source: autotag, decrypt and flatten-form share
// [runSimpleMutation] and declare no -o of their own, so a source-level scan
// keyed on the command's own function reports them as producing nothing.
//
// Measured when this test was written: 39 dispatchable, 20 declaring -o, being
// the 18 in stageCmds plus sign and redact. No asymmetry. This pins that rather
// than establishing it.
func TestEveryOutputProducingCommandIsAStageOrDeliberatelyExcluded(t *testing.T) {
	t.Parallel()

	dispatched := dispatchCommandNames(t)
	if len(dispatched) < 30 {
		t.Fatalf("found only %d dispatch cases, so the switch was not parsed", len(dispatched))
	}

	names := make([]string, 0, len(dispatched))
	for name := range dispatched {
		names = append(names, name)
	}
	sort.Strings(names)

	producing := make(map[string]bool)
	for _, name := range names {
		if commandDeclaresOutputFlag(t, name) {
			producing[name] = true
		}
	}

	// Controls. A probe that matched no flag line would find nothing
	// output-producing and report perfect agreement below, and one that matched
	// every line would make the whole check vacuous in the other direction.
	if len(producing) < 15 {
		t.Fatalf("only %d commands appear to declare -o, so the help was not parsed", len(producing))
	}
	for _, want := range []string{"rotate", "merge", "remediate"} {
		if !producing[want] {
			t.Fatalf("%q writes a document but the probe did not see its -o, so the comparison means nothing", want)
		}
	}
	for _, readOnly := range []string{"info", "text", "validate"} {
		if producing[readOnly] {
			t.Fatalf("%q is a read-only report yet the probe called it output-producing, "+
				"so the probe matches lines it should not", readOnly)
		}
	}

	for _, name := range names {
		if !producing[name] || stageCmds[name] {
			continue
		}
		if _, excluded := nonStageOutputCmds[name]; excluded {
			continue
		}
		t.Errorf("%q writes a document with -o but is neither a pipe stage nor listed in "+
			"nonStageOutputCmds, so `litho pipe ... %s ...` answers \"unknown stage\" for a "+
			"command litho ships", name, name)
	}

	// The exclusions have to stay honest too, or the set becomes a place to
	// silence this test.
	excluded := make([]string, 0, len(nonStageOutputCmds))
	for name := range nonStageOutputCmds {
		excluded = append(excluded, name)
	}
	sort.Strings(excluded)

	for _, name := range excluded {
		if !dispatched[name] {
			t.Errorf("nonStageOutputCmds names %q, which runCommand no longer dispatches", name)
		}
		if stageCmds[name] {
			t.Errorf("%q is listed as deliberately not a stage, but stageCmds now contains it: "+
				"one of the two is wrong", name)
		}
		// pipe is exempt: its help is hand-written prose rather than a FlagSet
		// dump, so the -o it accepts never appears as a flag row.
		if name != "pipe" && !producing[name] {
			t.Errorf("%q is listed as an output-producing non-stage but declares no -o, "+
				"so the entry is stale", name)
		}
	}

	// A stage that writes no document would be a stage with nothing to pass on.
	for name := range stageCmds {
		if !producing[name] {
			t.Errorf("stage %q declares no -o, so it cannot produce the document the next "+
				"stage reads", name)
		}
	}
}

// commandDeclaresOutputFlag reports whether a command's own help lists an -o
// flag, by running it in process the way [TestPipe_EveryBooleanStageFlagIsRegistered]
// inspects a stage's flags.
//
// Both writers are read: flag.PrintDefaults goes to the FlagSet's output, which
// commands point at stderr, while a command that prints its own usage may use
// stdout.
func commandDeclaresOutputFlag(t *testing.T, name string) bool {
	t.Helper()

	var stdout, stderr bytes.Buffer
	run(context.Background(), []string{name, "-h"}, &stdout, &stderr)

	for _, out := range []string{stdout.String(), stderr.String()} {
		for line := range strings.SplitSeq(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && fields[0] == "-o" {
				return true
			}
		}
	}
	return false
}
