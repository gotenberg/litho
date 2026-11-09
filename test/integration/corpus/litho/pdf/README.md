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
committing the PDFs is fine. The pikepdf builders in `build/` regenerate them
byte for byte.

## Layout

```
corpus/litho/pdf/
├── manifest.json   Corpus manifest (profile_mode=expand over 13 profiles, committed_pdfs=pdfs)
├── text-overrides.json  Corrections to the PDF.js text goldens, with reasons
├── build/          pikepdf builders, one per fixture, plus _common.py
├── pdfs/           The committed fixtures
├── FIXTURES.md     Expected firing sets and known divergences
└── README.md       This file
```

Nothing is downloaded: `corpus goldens` copies `pdfs/*.pdf` into the testdata
before the provisioning image derives the goldens.

## Regenerating

```bash
cd test/integration/corpus/litho/pdf/build
python3 complex_1b.py # rewrites ../pdfs/complex_1b.pdf
```

Builders save with a deterministic `/ID` and `stream_decode_level=none`, so a
rebuild that changes nothing produces an empty diff. The decode level matters:
qpdf's default silently rewrites an `LZWDecode` stream as Flate, which would
delete a planted violation.

`trailer_2b.pdf` is the one exception. It is encrypted, and qpdf derives a
random file encryption key on every save with no way to pin it, so successive
builds differ inside `/O`, `/U` and every encrypted string. Regenerate that
one deliberately rather than diffing it against a fresh build.

## Fixtures

[FIXTURES.md](FIXTURES.md) is the authoritative record of what each fixture plants, its expected firing set at each profile, and the known divergences from veraPDF. Each builder in `build/` repeats its fixture's intent in its docstring.
