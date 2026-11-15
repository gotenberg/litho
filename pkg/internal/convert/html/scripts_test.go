package html

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestScriptsOf_CountsWhatABrowserRuns counts, in documents of one
// construct each, the scripts a browser would run on its own: the script
// elements the HTML Standard, 4.12.1.1 runs, with Chromium's reading of a
// module type, the handlers of the events a page's load and printing
// fire, and a frame navigated to a javascript: URL.
func TestScriptsOf_CountsWhatABrowserRuns(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src  string
		want int
	}{
		{`<script>x()</script>`, 1},
		{`<script type="">x()</script>`, 1},
		{`<script type=" TEXT/JavaScript ">x()</script>`, 1},
		{`<script type="application/x-javascript">x()</script>`, 1},
		{`<script type="text/javascript1.5">x()</script>`, 1},
		{`<script language="JavaScript1.2">x()</script>`, 1},
		{`<script language="">x()</script>`, 1},
		{`<script type="text/javascript" language="vbscript">x()</script>`, 1},
		{`<script type="module">x()</script>`, 1},
		{`<script type="MODULE">x()</script>`, 1},
		{`<script src="a.js"></script>`, 1},
		{`<svg><script>x()</script><script type="text/ecmascript">y()</script></svg>`, 2},
		{`<body onload="x()" onpageshow="x()" onbeforeprint="x()">`, 3},
		{`<img src=a.png onload="x()" onerror="y()">`, 2},
		{`<link rel=stylesheet href=a.css onerror="x()">`, 1},
		{`<svg onload="x()"><image href=a.png onerror="y()"/></svg>`, 2},
		{`<iframe src="javascript:x()"></iframe>`, 1},
		{`<script type="text/javascript; charset=utf-8">x()</script>`, 0},
		{`<script type="application/ld+json">{}</script>`, 0},
		{`<script type="text/template"><p></p></script>`, 0},
		{`<script type="importmap">{"imports":{}}</script>`, 0},
		{`<script type=" module">x()</script>`, 0},
		{`<script type="module ">x()</script>`, 0},
		{`<script type="javascript">x()</script>`, 0},
		{`<script language="vbscript">x()</script>`, 0},
		{`<script nomodule>x()</script>`, 0},
		{`<script src="">x()</script>`, 0},
		{`<script>  </script>`, 0},
		{`<template><script>x()</script></template>`, 0},
		{`<noscript><script>x()</script></noscript>`, 0},
		{`<body onafterprint="x()"><div onload="x()" onerror="x()">d</div>`, 0},
		{`<img src=a.png onload="  ">`, 0},
		{`<a href="javascript:x()">a</a><object data="javascript:x()"></object>`, 0},
	} {
		doc, err := parse([]byte(tc.src))
		if err != nil {
			t.Fatal(err)
		}
		s, err := scanDocument(context.Background(), doc, documentURL, MaxElements)
		if err != nil {
			t.Fatal(err)
		}
		if s.scripts != tc.want {
			t.Errorf("%s: %d scripts, want %d", tc.src, s.scripts, tc.want)
		}
	}
}

// TestScriptWarning_OnceADocument checks that a document holding scripts
// reports one warning, of the omitted effect, saying how many did not run,
// and that one without reports none.
func TestScriptWarning_OnceADocument(t *testing.T) {
	t.Parallel()

	p := prep(t, `<body onload="x()"><script>a()</script><p>x<script src=b.js></script>`, Options{})
	var found []string
	for _, w := range p.warnings {
		if w.Code == warn.ScriptNotRun {
			found = append(found, w.Message)
			if w.Effect != warn.Omitted {
				t.Errorf("the warning %q has the effect %v", w.Message, w.Effect)
			}
		}
	}
	if len(found) != 1 || !strings.HasPrefix(found[0], "3 scripts did not run") {
		t.Errorf("got %q, want one warning that 3 scripts did not run", found)
	}
	p = prep(t, `<script type="application/ld+json">{}</script><p>x`, Options{})
	if hasWarning(p.warnings, warn.ScriptNotRun, "") {
		t.Error("a data block alone reports a script")
	}
}
