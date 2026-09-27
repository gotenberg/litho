package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// verifyPagesMove checks what [applyPages] promises: each output page carries
// the text of the input page it moved from (see [verifyIdentityText]) and
// shows the label that page showed (see [mutationFeature.checkPageLabels]).
func verifyPagesMove(ctx context.Context, f *mutationFeature, in *mutationInput, out *mutationOutputDoc, plan mutationPlan) []mutationFailure {
	failures := verifyIdentityText(ctx, f, in, out, plan)
	if failure, ok := f.checkPageLabels(ctx, in, out, plan); ok {
		failures = append(failures, failure)
	}
	return failures
}

// checkPageLabels reports an output page that does not show the label its
// input page showed, as promise_broken. A page keeps the label it displays
// through a page edit (ISO 32000-1, 12.4.2), and an input with no /PageLabels
// gives an output with none.
//
// qpdf reads the labels, not litho, so the check does not grade litho's
// writer with litho's reader. ok is false when qpdf is absent or cannot read
// either file, which the qpdf_failed check reports on its own.
func (f *mutationFeature) checkPageLabels(ctx context.Context, in *mutationInput, out *mutationOutputDoc, plan mutationPlan) (mutationFailure, bool) {
	if f.qpdf == "" {
		return mutationFailure{}, false
	}
	inLabels, err := qpdfPageLabels(ctx, f.qpdf, in.data, "", in.pages)
	if err != nil {
		return mutationFailure{}, false
	}
	outLabels, err := qpdfPageLabels(ctx, f.qpdf, out.data, plan.password, out.pages)
	if err != nil {
		return mutationFailure{}, false
	}
	switch {
	case inLabels == nil && outLabels == nil:
		return mutationFailure{}, false
	case inLabels == nil:
		return mutationFailure{Metric: metricPromiseBroken, Detail: "the input has no /PageLabels and the output has"}, true
	case outLabels == nil:
		return mutationFailure{Metric: metricPromiseBroken, Detail: "the output lost the input's /PageLabels"}, true
	}
	for i, source := range plan.pageSource {
		if i >= len(outLabels) || source >= len(inLabels) {
			break
		}
		if outLabels[i] != inLabels[source] {
			return mutationFailure{Metric: metricPromiseBroken, Detail: fmt.Sprintf(
				"output page %d shows the label %s, input page %d showed %s",
				i+1, outLabels[i], source+1, inLabels[source])}, true
		}
	}
	return mutationFailure{}, false
}

// qpdfLabelRange is one range of qpdf --json's "pagelabels": the page index it
// starts at and its page label dictionary, strings as qpdf's JSON writes them.
type qpdfLabelRange struct {
	Index int `json:"index"`
	Label struct {
		S  string `json:"/S"`
		P  string `json:"/P"`
		St int64  `json:"/St"`
	} `json:"label"`
}

// qpdfPageLabels returns the label each of the pages of data shows as qpdf
// reads its /PageLabels, one comparable string per page, or nil when qpdf
// finds no labels. A range numbers its pages from its /St (ISO 32000-1, Table
// 159); a label with no style shows no number, so none is compared.
func qpdfPageLabels(ctx context.Context, qpdf string, data []byte, password string, pages int) ([]string, error) {
	tmp, err := os.CreateTemp("", "litho-mutate-*.pdf")
	if err != nil {
		return nil, fmt.Errorf("qpdf staging file: %w", err)
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("writing the qpdf staging file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("closing the qpdf staging file: %w", err)
	}

	args := []string{"--json", "--json-key=pagelabels"}
	if password != "" {
		args = append(args, "--password="+password)
	}
	args = append(args, path)
	cmd := exec.CommandContext(ctx, qpdf, args...) //nolint:gosec // qpdf from LookPath, path is our staging file
	stdout, runErr := cmd.Output()
	// qpdf exits 3 when it read the file with warnings, and still writes the
	// JSON.
	var exitErr *exec.ExitError
	if runErr != nil && (!errors.As(runErr, &exitErr) || exitErr.ExitCode() != 3) {
		return nil, fmt.Errorf("running qpdf --json: %w", runErr)
	}
	var doc struct {
		PageLabels []qpdfLabelRange `json:"pagelabels"`
	}
	if err := json.Unmarshal(stdout, &doc); err != nil {
		return nil, fmt.Errorf("reading qpdf --json: %w", err)
	}
	if len(doc.PageLabels) == 0 {
		return nil, nil
	}

	ranges := doc.PageLabels
	sort.SliceStable(ranges, func(a, b int) bool { return ranges[a].Index < ranges[b].Index })
	labels := make([]string, pages)
	var current *qpdfLabelRange
	next := 0
	for i := range labels {
		for next < len(ranges) && ranges[next].Index <= i {
			current = &ranges[next]
			next++
		}
		if current == nil {
			labels[i] = "(none)"
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "S=%q P=%q", current.Label.S, current.Label.P)
		if current.Label.S != "" {
			fmt.Fprintf(&b, " N=%d", current.Label.St+int64(i-current.Index))
		}
		labels[i] = b.String()
	}
	return labels, nil
}
