package main

import (
	"errors"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// sampleAFM mirrors the layout of a Core 14 AFM file.
const sampleAFM = `StartFontMetrics 4.1
Comment Copyright (c) 1985, 1987, 1989, 1990, 1997 Adobe Systems Incorporated.  All Rights Reserved.
Comment Creation Date: Thu May  1 12:38:23 1997
FontName Helvetica
StartCharMetrics 2
C 32 ; WX 278 ; N space ; B 0 0 0 0 ;
C 65 ; WX 667 ; N A ; B 14 0 654 718 ;
C -1 ; WX 556 ; N fi ; B 7 0 512 728 ; L i fi ;
EndCharMetrics
EndFontMetrics
`

func TestParseAFM(t *testing.T) {
	a, err := parseAFM([]byte(sampleAFM), "Helvetica")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := a.bounds["A"], [4]int{14, 0, 654, 718}; got != want {
		t.Errorf("A = %v, want %v", got, want)
	}
	if got, want := a.bounds["fi"], [4]int{7, 0, 512, 728}; got != want {
		t.Errorf("fi = %v, want %v", got, want)
	}
	if !strings.HasPrefix(a.copyright, "Copyright (c) 1985") {
		t.Errorf("copyright = %q", a.copyright)
	}
}

func TestParseAFMRefusesWhatItCannotTranscribe(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		font string
		want error
	}{
		{"wrong font", sampleAFM, "Times-Roman", errFontName},
		{"no copyright", strings.Replace(sampleAFM, "Comment Copyright", "Comment Notice", 1), "Helvetica", errNoCopyright},
		{"no metrics", "FontName Helvetica\nComment Copyright x\n", "Helvetica", errNoCharMetrics},
		{"no box", strings.Replace(sampleAFM, " ; B 14 0 654 718", "", 1), "Helvetica", errCharMetric},
		{"short box", strings.Replace(sampleAFM, "B 14 0 654 718", "B 14 0 654", 1), "Helvetica", errCharMetric},
		{"no name", strings.Replace(sampleAFM, "N A ;", "", 1), "Helvetica", errCharMetric},
		{"duplicate", strings.Replace(sampleAFM, "N fi", "N A", 1), "Helvetica", errDuplicateName},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseAFM([]byte(tc.data), tc.font); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestGenerateWritesValidGo(t *testing.T) {
	a, err := parseAFM([]byte(sampleAFM), "Helvetica")
	if err != nil {
		t.Fatal(err)
	}
	parsed := make(map[string]afm, len(fonts))
	for _, name := range fonts {
		parsed[name] = a
	}
	code, err := generate(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), outputName, code, parser.ParseComments); err != nil {
		t.Fatalf("generated source does not parse: %v", err)
	}
	for _, want := range []string{`"A":`, "{14, 0, 654, 718}", "that this paragraph is not modified"} {
		if !strings.Contains(string(code), want) {
			t.Errorf("generated source lacks %q", want)
		}
	}
}
