# Content editing: annotations and page geometry in litho

Plan for two related "edit the page" capabilities litho lacks: authoring annotations, and page geometry operations (crop, scale, N-up / imposition).

litho's writer can transform a document structurally (rotate, delete, insert, split, merge, stamp) but cannot add interactive markup or change page geometry beyond rotation. These are grouped because both are additive writer operations built on the same mutation pipeline.

> **Lowest priority of the seven; mostly general-toolkit, not conformance.** Crop/scale/N-up/booklet/imposition are page-layout features of a general PDF toolkit (pdfcpu/pdftk territory) with **no conformance payoff**. The only conformance-adjacent hooks are `FlattenAnnotations` (PDF/X forbids most interactive annotations) and `/Redact` annotation authoring (feeds [REDACTION.md](REDACTION.md)). Build the rest only on explicit user demand. (Mirrors [RENDERING.md](RENDERING.md)'s honest defer framing.)

## Part A — Annotation authoring

### What it is

Create annotation objects — links, highlights/underlines/strikeouts, text notes, free-text, line/rect/polygon, stamps-as-annotations, and redaction marks — with correct `/Annots` wiring and generated appearance streams (`/AP`) so non-interactive viewers and print show them.

### What exists today

| Capability                                              | Status                                                                                             |
| ------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| Annotation _reading_ (subtype, rect, contents, links)   | Done — `pkg/internal/annotation/annotation.go`, `ExtractAnnotations`                               |
| Content-stream _generation_ (text/vector → content)     | Done in spirit — `StampText`/`StampImage` build content streams; the same drawing code feeds `/AP` |
| Writer mutation pipeline + object allocation            | Done — `mutations`, `ObjAllocator`                                                                 |
| **Creating annotation dictionaries + `/Annots` wiring** | **Missing**                                                                                        |
| **`/AP` appearance generation for annotations**         | **Missing** (distinct from stamping page content)                                                  |

The difference from stamping: a stamp draws into page content; an annotation is a first-class object in `/Annots` with its own appearance, flags, and (for markup) author/date/reply structure.

### Design

- **`Writer.AddAnnotation(ctx, pageNum, annot)`** with a typed `Annotation` union (Link, Highlight, Text, FreeText, Line, Square, Stamp, Redact…). Build the dict, generate `/AP`, append to the page `/Annots`.
- **`/AP` coordinate space differs from stamping.** An appearance stream is a Form XObject whose content is **`/BBox`-relative** and mapped onto the annotation `/Rect` via `/Matrix` — not page-space like a stamp. The stamp _drawing_ code is reusable, but the spec must compute `/BBox`/`/Matrix` and emit BBox-relative coordinates, not the page coordinates `StampText` uses.
- **Link convenience:** `AddLink(page, rect, dest|uri)` — the highest-value case (TOC/cross-ref links), reusing the destination resolution litho already has for bookmarks.
- **Redaction marks** feed [REDACTION.md](REDACTION.md) (a `/Redact` annotation is just an authored annotation that `Redact` later applies).
- **`Writer.FlattenAnnotations`** (companion to `FlattenForm`) bakes annotation `/AP` into content for PDF/X.

## Part B — Page geometry (crop, scale, N-up, imposition)

### What it is

- **Crop:** set `/CropBox` (and `/TrimBox`/`/BleedBox`) to trim the visible/printable area.
- **Scale:** resize pages by adjusting `/MediaBox` and wrapping content in a scaling matrix.
- **N-up:** place N source pages as form XObjects on one larger sheet (2-up, 4-up…).
- **Imposition / booklet:** N-up plus page reordering for folding/binding (saddle-stitch order).

### What exists today

| Capability                                   | Status                                                                                                                                                                                               |
| -------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Page-box parsing + `models.Page` exposure    | Done — `catalog.go`, surfaced on `models.Page`                                                                                                                                                       |
| Page-tree manipulation                       | Done — `RotatePages`/`InsertPages`/`DeletePages` (Writer methods) and the top-level `Merge(ctx, sources, dst)` function (`pdf.Merge`) + `Pipeline.Merge` — **note `Merge` is not a `Writer` method** |
| Placing a page as a form XObject             | Done in spirit — `StampPDF` overlays a PDF page (`pageToFormXObject`); N-up tiles the same primitive                                                                                                 |
| **Setting CropBox/TrimBox/BleedBox**         | **Missing** (writer side; read side exists)                                                                                                                                                          |
| **Page scaling (MediaBox + content matrix)** | **Missing**                                                                                                                                                                                          |
| **N-up tiling + imposition reordering**      | **Missing**                                                                                                                                                                                          |

### Design

- **`Writer.SetPageBoxes(ctx, pages, boxes)`** — crop by writing the box arrays (validate Crop ⊆ Media, etc.).
- **`Writer.ScalePages(ctx, pages, factor|targetSize)`** — wrap each page's content in a `cm` matrix and resize MediaBox; scale annotation/link rects to match.
- **`Writer.NUp(ctx, n, sheetSize, opts)`** — convert each source page to a form XObject (reuse the `StampPDF` `pageToFormXObject` path), tile N per sheet with margins/gutters.
- **`Writer.Booklet(ctx, opts)`** — N-up=2 with saddle-stitch reordering.
- The new work is geometry math and page-tree reconstruction, not new low-level machinery.

## Plan

- **PR1 — Page boxes + scaling.** `SetPageBoxes`, `ScalePages` (with annotation-rect handling).
- **PR2 — Annotation authoring core.** `AddAnnotation` + BBox-relative `/AP` generation + `AddLink`; `FlattenAnnotations`. (The two conformance-relevant pieces.)
- **PR3 — N-up + imposition.** Tiling via page-as-XObject; booklet reordering.
- **PR4 — CLI.** `litho crop`, `litho scale`, `litho nup`, `litho annotate`/`litho link`.

## Limits and honest scope

- **N-up drops source annotations and form fields.** `pageToFormXObject` (`mutations/stamp_pdf.go`) copies page _content_, **not** `/Annots` — so tiling silently loses links, fields, and markup unless PR3 adds a pass that re-creates `/Annots` with rects mapped through the tiling matrix. State this limit, or build the re-creation pass.
- **Scaling rasterized content** stays raster; only vector content scales cleanly.
- **N-up of mismatched page sizes / rotations** needs a fit policy (fit/fill/center); expose it, don't guess silently.
- **Annotation appearance fidelity** for rich markup is litho-generated — "correct and printable," not "Acrobat-pixel-identical."

## Out of scope

- **XFA form authoring/rendering.** litho _detects_ XFA (`annotation/xfa.go`) and reads AcroForm; it does not author or render XFA (deprecated in PDF 2.0).
- Interactive editing UI; this is a programmatic writer API.
- Rich-media / 3D annotation authoring (read/validate only — see [PDF-E.md](PDF-E.md)).
