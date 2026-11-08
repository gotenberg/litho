# libreoffice/csv corpus fixtures

Comma-separated values files scored by the `diff` feature (see `FEATURES.md`) against LibreOffice Calc as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. A selection of LibreOffice's Calc CSV tests at the commit `LIBREOFFICE_CORE_COMMIT` pins, of which only the list and the hashes are committed; Gotenberg's own CSV files are the `gotenberg/csv` corpus. The references are built on the host and never committed. The corpus works as the libreoffice/xlsx corpus does: its `FIXTURES.md` documents the scripts, the provisioning and the fonts, and the libreoffice/docx corpus's the reference checks, which apply here with `csv` for `xlsx` (`make corpus-fetch CORPUS_NAME=libreoffice/csv`, `make office-references SOURCES=libreoffice FORMATS=csv`).

The route loads a CSV with LibreOffice's import filter options `44/59/9,34,76,1,,0,false,true,true,false`: fields split at commas, semicolons and tabs alike, quoted with `"`, read as UTF-8, every column's type detected, numbers and dates detected, quoted numbers still numbers, spaces kept. It then turns the page header off on every page style of the document, since Calc's default header prints the sheet name, which is the upload's file name; the footer keeps Calc's default, the page number. Every reference prints on Letter paper, LibreOffice's default for the image's locale.

| Path             | What it holds                                                                                  |
| ---------------- | ---------------------------------------------------------------------------------------------- |
| `documents.json` | The 9 LibreOffice test files, each by its ID, `sc/<file>.csv` (`sc/tdf148423.csv`)             |
| `files.sha256`   | Every fetched file, `<sha256>  <path in LibreOffice core>`: the 9 files and `COPYING.MPL`      |
| `download.sh`    | Installs the references from the testdata volume as the corpus PDFs the diff goldens come from |

`sc/<file>.csv` is `sc/qa/unit/data/csv/<file>.csv`.

## How the documents were chosen

`corpus select` reads the `.csv` files of LibreOffice core's `sc/qa/unit/data/csv` at the pinned commit and keeps one when it is at most 150 KiB and its name is plain ASCII (letters, digits, dot, underscore, hyphen); a text file needs no other check. The route converts all of them.

| Directory             | Files | Qualifying | Cap |
| --------------------- | ----- | ---------- | --- |
| `sc/qa/unit/data/csv` | 9     | 9          | 9   |

LibreOffice's test documents are under the Mozilla Public License 2.0 (some also under the LGPL 3.0 or later, `COPYING.MPL` is fetched beside them): they are downloaded for testing and never committed or redistributed.

## Fonts

Every reference draws Liberation Sans, Calc's default, which the fixture fonts hold.

## How the rig was proven

Each reference handed to the cell as litho's conversion, scored against the goldens made from it, passes on all 11: pages, text, links, outline and the strict rule score 1 on every one, and layout averages 0.970. `TestDiffFeature_LibreOfficeReferencesScoreOneAgainstTheirGoldens` checks the committed sources so on every run with the corpus provisioned. The counts cover this corpus and `gotenberg/csv` together.

## Accepted disagreements

LibreOffice names a CSV's one sheet after the file it loads, which the route stores under a UUID: the reference's outline entry is that UUID, so the cell matches a CSV fixture's outline items by page alone, and litho names the sheet `Sheet1`, as Calc names a new one.
