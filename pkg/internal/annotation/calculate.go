package annotation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/dop251/goja"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// maxCalculationTime bounds the time the calculation scripts of one document
// run, its document-level scripts included. It is an anti-DoS guard, not
// spec-mandated: a script is a program, and `while(true){}` in a field's
// calculate action (ISO 32000-1, 12.6.3, Table 196) ran until the process
// ended, whatever the caller's context said. A real calculation is arithmetic
// over a few fields that runs in well under a millisecond, so one second is
// far above any form's.
const maxCalculationTime = time.Second

// errCalculationStopped is the value the calculation VM is interrupted with
// when the caller's context is done or [maxCalculationTime] is spent.
var errCalculationStopped = errors.New("form calculation stopped")

// runCalculations runs the calculation scripts of scripts, keyed by the fully
// qualified name of their field, and sets each field's value in formData to
// what its script computes. A script reads the other fields' values through
// this.getField(name).value and writes its own to event.value, the part of the
// Acrobat JavaScript API calculations use.
//
// Scripts run in document order, as the order argument lists the fields, then
// the remaining names sorted, so the values a calculation reads from another
// do not depend on the order a Go map yields them in.
//
// The scripts are best effort: one that throws, or reads a field or a global
// litho does not provide, leaves its field's value as the document stores it.
// They run under the caller's context and [maxCalculationTime], and the VM is
// interrupted when either is done:
//
//   - A done context returns its error, as every other walk of the document
//     does: the caller asked to stop.
//   - A spent budget discards every computed value and returns nil. The values
//     the document stores are still the form's, and a script that runs too long
//     must not deny the caller the fields themselves, which is what listing a
//     form is for. Keeping the values computed before the budget ran out would
//     make the answer depend on how fast the machine ran them.
func runCalculations(ctx context.Context, doc *document.Document, scripts map[string]string, order []string, formData map[string]string) error {
	if len(scripts) == 0 {
		return nil
	}

	values := maps.Clone(formData)
	vm := goja.New()

	budget, cancel := context.WithTimeout(ctx, maxCalculationTime)
	defer cancel()
	// The watcher calls Interrupt alone, which only sets a flag the VM polls,
	// and returns before runCalculations does.
	done := make(chan struct{})
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		select {
		case <-budget.Done():
			vm.Interrupt(errCalculationStopped)
		case <-done:
		}
	}()
	defer func() {
		close(done)
		<-watched
	}()

	// stopped reports whether the VM was interrupted, and the error to return
	// for it: the caller's, or nil for a spent budget.
	stopped := func() (bool, error) {
		if budget.Err() == nil {
			return false, nil
		}
		if err := ctx.Err(); err != nil {
			return true, fmt.Errorf("running form calculations: %w", err)
		}
		doc.Logger.Debug("form calculations exceeded their time budget; keeping the stored values",
			slog.Duration("budget", maxCalculationTime))
		return true, nil
	}

	// getField hands a script a field object carrying the value the field
	// holds now, the one the document stores or a calculation run before. A
	// script calls it as this.getField: at the top level of a script, this is
	// the global object, which holds getField.
	//
	// goja Set errors are dropped: setting a global or a property of a plain
	// object cannot fail.
	_ = vm.Set("getField", func(call goja.FunctionCall) goja.Value { //nolint:errcheck // Best-effort JS execution; setting a global cannot fail.
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		field := vm.NewObject()
		_ = field.Set("value", values[call.Arguments[0].String()]) //nolint:errcheck // Best-effort JS execution; setting a property of a plain object cannot fail.
		return field
	})

	docScripts, scriptsErr := ExtractDocumentScripts(ctx, doc)
	if scriptsErr != nil {
		// Not a goja failure like the calls around it. This one is the
		// name-tree walk declining to yield scripts at all, a malformed /Names
		// dictionary or a tree past maxDocumentScriptsSize. Field calculations
		// still run, they just run without the document-level scripts.
		doc.Logger.Debug("extracting document-level JavaScript; continuing without it",
			slog.Any("err", scriptsErr))
	}
	for _, script := range docScripts {
		_, _ = vm.RunString(script) //nolint:errcheck // Best-effort JS execution; PDF scripts commonly reference undefined globals.
		if halt, err := stopped(); halt {
			return err
		}
	}

	for _, name := range calculationOrder(scripts, order) {
		_, _ = vm.RunString(`var event = { value: "" };`) //nolint:errcheck // Best-effort JS execution; the bootstrap cannot fail.
		_, runErr := vm.RunString(scripts[name])
		if halt, err := stopped(); halt {
			return err
		}
		if runErr != nil {
			continue
		}
		// Read in the VM, where the interrupt reaches a toString a script gave
		// event.value, and where an event a script replaced with null throws
		// rather than panicking in Go.
		res, err := vm.RunString(`String(event.value)`)
		if halt, err := stopped(); halt {
			return err
		}
		if err != nil {
			continue
		}
		if v := res.String(); v != "" && v != "undefined" {
			values[name] = v
		}
	}

	maps.Copy(formData, values)
	return nil
}

// calculationOrder returns the names of scripts as the order argument lists
// them, then the ones it does not list, sorted.
func calculationOrder(scripts map[string]string, order []string) []string {
	names := make([]string, 0, len(scripts))
	listed := make(map[string]bool, len(scripts))
	for _, name := range order {
		if _, ok := scripts[name]; ok && !listed[name] {
			listed[name] = true
			names = append(names, name)
		}
	}
	var rest []string
	for name := range scripts {
		if !listed[name] {
			rest = append(rest, name)
		}
	}
	slices.Sort(rest)
	return append(names, rest...)
}
