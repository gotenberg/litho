# govdocs corpus fixtures

Govdocs1 is a corpus of a million files Digital Corpora gathered from United States government web sites, redistributed in thread archives of about a thousand files each. Its corpora sample the threads `GOVDOCS_THREADS` in `corpus/versions.env` pins:

| Corpus        | Fixtures                                | Scored against                                                     |
| ------------- | --------------------------------------- | ------------------------------------------------------------------ |
| `govdocs/pdf` | Every PDF of the threads, about 1,100   | PDF.js, veraPDF and the other PDF oracles; downloaded in the image |
| `govdocs/doc` | 100 Word 97 to 2003 documents           | LibreOffice, by the `diff` feature                                 |
| `govdocs/xls` | 100 Excel 97 to 2003 workbooks          | LibreOffice, by the `diff` feature                                 |
| `govdocs/ppt` | 100 PowerPoint 97 to 2003 presentations | LibreOffice, by the `diff` feature                                 |

The pinned threads hold no OOXML document: these are the real documents the binary formats were used for, where the poi corpora hold test cases. litho reads none of the binary formats yet: the three Office cells score a convert error on every document until it does.

The Office corpora work as the libreoffice corpora do. Only the lists and the hashes are committed: `make corpus-fetch CORPUS_NAME=govdocs` downloads the documents and `make office-references SOURCES=govdocs` builds their references in the testdata.

| Path                      | What it holds                                                                                                                            |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `<format>/documents.json` | The documents, each `{"id", "path"}`: the ID `<thread>/<file>`, the path `<thread>.zip/<entry>`                                          |
| `<format>/files.sha256`   | `<sha256>  <path>` for each document                                                                                                     |
| `<format>/manifest.json`  | The corpus manifest; `references.source_pin` names `GOVDOCS_THREADS`                                                                     |
| `corpus select`           | `make corpus-select CORPUS_NAME=govdocs/<format>`: rewrites the two lists above, reading the thread archives through HTTP range requests |
| `corpus fetch`            | `make corpus-fetch CORPUS_NAME=govdocs`: reads the listed entries out of the thread archives with HTTP range requests, hash-checked      |

## How the documents were chosen

`corpus select` lists the archives' files of the format's extension of at most 2 MiB (real documents run larger than test cases: the threads' presentations average 3 MB) and takes them in the order of the SHA-256 of their path, so the choice stays put when the caps change. It downloads each, and keeps it when it qualifies as `corpus select` (`cmd/corpus/select_qualify.go`) decides (see `poi/FIXTURES.md`), until 100 are kept: Word 6 and 95 documents, BIFF5 workbooks, encrypted files and files with macros or embedded objects are passed over.
