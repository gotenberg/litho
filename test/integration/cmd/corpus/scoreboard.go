package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Scoreboard aggregates results across every (corpus, feature) cell
// the runner traversed. Serialised as JSON or rendered as text.
type Scoreboard struct {
	Corpora []*CorpusReport `json:"corpora"`
	Elapsed string          `json:"elapsed"`
}

// CorpusReport holds one corpus row of the matrix plus per-feature
// results.
type CorpusReport struct {
	Name     string           `json:"name"`
	Fixtures int              `json:"fixtures"`
	Features []*FeatureReport `json:"features"`
}

// FeatureReport captures the outcome of running one feature across
// every fixture in a corpus.
type FeatureReport struct {
	Name             string         `json:"name"`
	GoldenSource     string         `json:"golden_source"`
	NoGolden         bool           `json:"no_golden,omitempty"`
	LoadError        string         `json:"load_error,omitempty"`
	Matched          int            `json:"matched"`
	Mismatched       int            `json:"mismatched"`
	Skipped          int            `json:"skipped"`
	NoGoldenFixtures int            `json:"no_golden_fixtures,omitempty"`
	Metrics          map[string]int `json:"metrics,omitempty"`
	Verdicts         []Verdict      `json:"verdicts,omitempty"`
}

// Denominator returns the number of fixtures that counted toward the
// match/mismatch split (excludes skipped and missing-golden cases).
func (fr *FeatureReport) Denominator() int {
	return fr.Matched + fr.Mismatched
}

// PrintText renders a human-readable scoreboard on w.
func (s *Scoreboard) PrintText(w io.Writer) {
	fmt.Fprintln(w, "litho corpus scoreboard")
	fmt.Fprintln(w, "=======================")
	fmt.Fprintf(w, "Elapsed: %s\n\n", s.Elapsed)

	for _, c := range s.Corpora {
		fmt.Fprintf(w, "Corpus: %s (%d fixtures)\n", c.Name, c.Fixtures)
		for _, fr := range c.Features {
			printFeatureRow(w, fr)
		}
		fmt.Fprintln(w)
	}

	printMismatches(w, s)
	printRuleSetMismatches(w, s)
}

func printFeatureRow(w io.Writer, fr *FeatureReport) {
	prefix := fmt.Sprintf("  %-22s [%s]", fr.Name, fr.GoldenSource)
	switch {
	case fr.LoadError != "":
		fmt.Fprintf(w, "%s load error: %s\n", prefix, fr.LoadError)
		return
	case fr.NoGolden:
		fmt.Fprintf(w, "%s [no golden]\n", prefix)
		return
	}

	denom := fr.Denominator()
	primary := fmt.Sprintf("%d/%d (%.1f%%)", fr.Matched, denom, pct(fr.Matched, denom))

	switch fr.Name {
	case "text":
		chars := fr.Metrics["chars_match"]
		words := fr.Metrics["words_match"]
		fmt.Fprintf(w, "%s chars: %d/%d (%.1f%%)  words: %d/%d (%.1f%%)\n",
			prefix, chars, denom, pct(chars, denom), words, denom, pct(words, denom))
	case "compliance":
		fp := fr.Metrics["false_positive"]
		fn := fr.Metrics["false_negative"]
		rs := fr.Metrics["ruleset_mismatch"]
		fmt.Fprintf(w, "%s %s  FP=%d FN=%d  ruleset_mismatch=%d\n", prefix, primary, fp, fn, rs)
	case "remediation", "remediation-aggressive":
		// Report the metrics the feature actually emits. No feature ever wrote
		// "regression" or "improved", so both columns printed 0 for every
		// corpus on every run, which reads as a measured guarantee of zero
		// regressions instead of as a missing measurement.
		fullyFixed := fr.Metrics["fully_fixed"]
		lithoRegressed := fr.Metrics["litho_regressed"]
		veraRegressed := fr.Metrics["verapdf_regressed"]
		fmt.Fprintf(w, "%s %s  fully_fixed=%d  litho_regressed=%d  verapdf_regressed=%d\n",
			prefix, primary, fullyFixed, lithoRegressed, veraRegressed)
	default:
		fmt.Fprintf(w, "%s %s%s\n", prefix, primary, firedMutationMetrics(fr))
	}

	if fr.NoGoldenFixtures > 0 {
		fmt.Fprintf(w, "    %d fixtures skipped (no per-fixture golden)\n", fr.NoGoldenFixtures)
	}
	if fr.Skipped > 0 {
		fmt.Fprintf(w, "    %d fixtures skipped (corpus skip list or not scored)\n", fr.Skipped)
	}
}

// firedMutationMetrics names the failure counts of a mutate-* cell that fired,
// so its row reads as which check failed rather than as a bare count. It
// returns "" for any other feature.
func firedMutationMetrics(fr *FeatureReport) string {
	if !strings.HasPrefix(fr.Name, "mutate-") {
		return ""
	}
	var fired []string
	for name, n := range fr.Metrics {
		if n > 0 {
			fired = append(fired, fmt.Sprintf("%s=%d", name, n))
		}
	}
	if len(fired) == 0 {
		return ""
	}
	sort.Strings(fired)
	return "  " + strings.Join(fired, " ")
}

// maxTextMismatches caps the fixtures listed per cell in the text
// scoreboard so the output stays readable; --format=json carries the full
// list.
const maxTextMismatches = 20

func printMismatches(w io.Writer, s *Scoreboard) {
	anyMismatches := false
	for _, c := range s.Corpora {
		for _, fr := range c.Features {
			if fr.Mismatched == 0 {
				continue
			}
			anyMismatches = true
			fmt.Fprintf(w, "%s / %s mismatches (%d):\n", c.Name, fr.Name, fr.Mismatched)
			losers := make([]Verdict, 0, fr.Mismatched)
			for _, v := range fr.Verdicts {
				if v.Match || v.Skipped {
					continue
				}
				losers = append(losers, v)
			}
			sort.Slice(losers, func(i, j int) bool {
				return losers[i].FixtureID < losers[j].FixtureID
			})
			for i, v := range losers {
				if i >= maxTextMismatches {
					fmt.Fprintf(w, "  ... %d more\n", len(losers)-i)
					break
				}
				reason := v.Reason
				if reason == "" {
					reason = "mismatch"
				}
				fmt.Fprintf(w, "  %s: %s\n", v.FixtureID, reason)
			}
			fmt.Fprintln(w)
		}
	}
	if !anyMismatches {
		fmt.Fprintln(w, "No mismatches.")
	}
}

// printRuleSetMismatches lists the cells whose failed-rule set differs from
// the golden's. [printMismatches] lists verdict mismatches only, and a cell
// can agree on the verdict while it disagrees on the rules, which is the
// divergence the ruleset_mismatch metric counts.
func printRuleSetMismatches(w io.Writer, s *Scoreboard) {
	for _, c := range s.Corpora {
		for _, fr := range c.Features {
			n := fr.Metrics["ruleset_mismatch"]
			if n == 0 {
				continue
			}
			fmt.Fprintf(w, "\n%s / %s rule-set mismatches (%d):\n", c.Name, fr.Name, n)
			shown := 0
			for _, v := range fr.Verdicts {
				if v.Metrics["ruleset_mismatch"] == 0 {
					continue
				}
				if shown == maxTextMismatches {
					fmt.Fprintf(w, "  ... %d more\n", n-shown)
					break
				}
				fmt.Fprintf(w, "  %s: %s\n", v.FixtureID, v.Reason)
				shown++
			}
		}
	}
}
