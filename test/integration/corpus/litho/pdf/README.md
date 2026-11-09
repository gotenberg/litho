# litho/pdf corpus

Self-authored conformance fixtures, committed to git rather than downloaded.

Every other corpus comes from a third party and is dominated by files that
exercise one construct at a time. The veraPDF corpus in particular is
single-rule-per-file. That leaves a coverage hole this corpus fills:

> Does a firing rule mask a rule that should also fire?

Each fixture packs many **independent** violation classes into one small file.
If litho short-circuits, latches, or scopes a check too narrowly, the rule set
it reports diverges from veraPDF's and the fixture exposes it.

The fixtures are tiny, self-authored, and carry no third-party licence, so
committing the PDFs is fine, and unit tests across `pkg/` read them. The Go
builders in `build/` regenerate them byte for byte.

## Layout

```
corpus/litho/pdf/
├── manifest.json   Corpus manifest (profile_mode=expand over 13 profiles, committed_pdfs=pdfs)
├── text-overrides.json  Corrections to the PDF.js text goldens, with reasons
├── build/          Go builders, one per fixture, on test/integration/fixture/pdfgen
├── pdfs/           The committed fixtures
├── FIXTURES.md     Expected firing sets and known divergences
└── README.md       This file
```

Nothing is downloaded: `corpus goldens` copies `pdfs/*.pdf` into the testdata
before the provisioning image derives the goldens.

## Regenerating

```bash
make corpus-build CORPUS=litho/pdf
```

writes every fixture its builder makes into `pdfs/`, leaving a file that
already holds those bytes untouched. `TestBuildersMatchCommitted` holds every
committed PDF to its builder byte for byte, so a builder changed without its
PDF, or a PDF changed without its builder, fails `go test`. Then run
`make corpus-prepare CORPUS=litho/pdf`: it notices the changed PDFs and derives
the corpus's goldens again.

The builders write through `pdfgen` (`test/integration/fixture/pdfgen`), a
standard-library PDF writer that serialises as qpdf 12.2 does with a
deterministic `/ID` and no stream decoding, the settings the fixtures were
first written with through pikepdf: a rebuild that changes nothing produces an
empty diff, and no `LZWDecode` stream is silently rewritten as Flate, which
would delete a planted violation. `pdfgen` does not depend on litho's own
`pkg/`, so the fixtures are not written by the code they test. `build/README.md`
covers adding or changing a fixture.

## Fixtures

[FIXTURES.md](FIXTURES.md) is the authoritative record of what each fixture plants, its expected firing set at each profile, and the known divergences from veraPDF. Each builder in `build/` repeats its fixture's intent in its doc comment.
