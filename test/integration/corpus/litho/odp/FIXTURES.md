# litho/odp corpus fixtures

OpenDocument presentations that lock how a conversion applies the paper, the orientation and the notes pages to a deck, scored by the `diff` feature (see `FEATURES.md`) against LibreOffice Impress. No corpus of LibreOffice's test documents carries the route's options.

Each source is a directory, `sources/<name>/`, holding one flat OpenDocument file, plain XML that lives in git, and optionally `options.json`, the LibreOffice route's form fields it converts with (`fixture.LoadOffice` reads them as for the other Office corpora: `paperWidth` and `paperHeight` in inches, `landscape`, `nativePages`, `nativeBookmarks`, `exportNotesPages`). A source that differs from another by its options alone links that source's document. The references are LibreOffice's, built by `corpus references` with the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2) through `/forms/libreoffice/convert`, into the testdata, never committed. The documents draw Liberation Serif and Liberation Sans, which the image and the fixture fonts both carry, and were written by a script for this corpus.

| Source                    | Pages (LibreOffice) | Options                              | Exercises                                                                                                     |
| ------------------------- | ------------------- | ------------------------------------ | ------------------------------------------------------------------------------------------------------------- |
| `wide-deck`               | 3                   | none                                 | Three slides of 28 by 15.75 cm, each a page of the slide size                                                 |
| `wide-deck-paper`         | 3                   | `paperWidth: 8.5`, `paperHeight: 11` | The caller's paper: litho scales each slide down to fit Letter, centred; LibreOffice keeps the slide size     |
| `portrait-deck`           | 3                   | none                                 | Portrait slides, 7.5 by 10 inches                                                                             |
| `portrait-deck-landscape` | 3                   | `landscape`                          | Landscape asked of portrait slides: litho turns the page and fits the slide; LibreOffice keeps the slide size |
| `notes-pages`             | 6                   | `exportNotesPages`                   | Notes pages after the slides: Impress prints them on Letter, whatever the notes master's A4 page layout says  |

## Accepted disagreements

Impress applies neither the route's paper nor its landscape to a presentation: it prints each slide on a page of the slide size. litho follows its own rule, the same in every format: the caller's paper is every page, and a slide is scaled down to fit it, never up, and centred. `wide-deck-paper` and `portrait-deck-landscape` score that difference in their `pages` and layout cells.
