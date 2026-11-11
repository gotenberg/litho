# 1. Paper and the printed page

A document meets its reader on a sheet of paper or on a screen that pretends to be one. The size of that sheet decides where every line ends, where every page breaks and how much of a table fits across. A converter that reads the size wrong moves every word that follows, so the paper is the first thing a faithful conversion must get right.

Two families of sizes share the world. The ISO 216 series starts from a sheet of one square metre whose sides keep the ratio of one to the square root of two, so that halving a sheet gives the next size with the same shape. North America keeps the inch sizes of its paper mills: Letter, Legal, Tabloid and Executive, whose shapes owe nothing to one another.

Orientation turns a sheet on its side. A portrait page is taller than it is wide; a landscape page is the same sheet turned a quarter, which suits a wide table, a chart with many columns or a slide. The sheet does not change, only the direction the text runs across it.

## 1.1 How a section states its paper

A word processor stores the page size with each section of the document, together with its margins, its header and its footer. When a section ends, the next one may change the size, turn the page or both, so a single file can mix a portrait report with a landscape appendix printed on a larger sheet.

> The page is the unit of reading. A reader remembers that a passage sat at the top of a left-hand page long after the words themselves have faded, and a converter that moves the break moves that memory with it.

A spreadsheet states its paper per sheet, by a numbered code from a table of paper sizes, with an orientation and a scale. A presentation states one slide size for the whole deck, and each slide is drawn on a page of exactly that size.

![Three coloured shapes in a frame](picture.png)

_A picture placed inline with the text keeps its size on every paper._

# 2. Measures that follow the width

The measure is the length of a line of text. On a narrow sheet such as A5 the measure shrinks and a paragraph takes more lines; on a wide sheet such as A3 in landscape the measure grows and the same paragraph takes fewer. Every page break after the first moves accordingly, which is why a document must be laid out again, not scaled, when the paper changes.

Headings keep with the paragraph that follows them, so a heading never sits alone at the foot of a page. Tables split between rows, and a row that fits on no page is cut where the page ends. Pictures stay with the line that holds them and push that line to the next page when they do not fit.

| Paper | Series | Width | Height |
|---|---|---|---|
| A3 | ISO 216 | 297 mm | 420 mm |
| A4 | ISO 216 | 210 mm | 297 mm |
| A5 | ISO 216 | 148 mm | 210 mm |
| B5 | ISO 216 | 176 mm | 250 mm |
| Letter | ANSI | 8.5 in | 11 in |
| Legal | ANSI | 8.5 in | 14 in |
| Tabloid | ANSI | 11 in | 17 in |
| Executive | ANSI | 7.25 in | 10.5 in |

The table above takes the whole width of the text column, so its columns grow and shrink with the paper while its rows keep their height. A narrow sheet wraps the longer cells onto a second line; a wide sheet leaves room to spare.

## 2.1 Headers, footers and page numbers

The header and the footer repeat on every page of a section. The footer of this document carries the number of the page and the count of pages, which both change with the paper: a document that takes three pages on Letter may take five on A5 and two on A3.

Margins stay fixed when the paper changes, so the text column gains or loses exactly what the sheet gains or loses. One inch on each side leaves a column of six and a half inches on Letter, a little more than six inches on A4 and barely four on A5.

> Executive paper, seven and a quarter inches by ten and a half, is the smallest of the common American sizes and the one most often forgotten by software that lists only Letter and Legal.

# 3. Checking a conversion

A conversion is judged by how it looks. The reference engine prints the document on the paper it names; the converter prints it on the same paper; both are drawn and compared page by page. The size of each page must agree to within a point, the count of pages must agree, and the ink on each page must fall in the same places.

A mistake in reading the size shows at once on the page box of the very first page. A mistake in laying text out at another width shows only further down, where a line breaks one word early and every later line follows. Both are found by printing the same document on many papers, in both orientations, and comparing every page.

The papers covered here are A3, A4, A5 and B5 from the ISO series, and Letter, Legal, Tabloid and Executive from the American sizes, each in portrait and in landscape. Sixteen documents, each a copy of this one with only its paper changed, cover the whole range from the smallest to the largest common sheet.

When the caller names a paper, it replaces the paper of every section, and the landscape switch turns every page that is not already turned. A document that names no paper at all prints on the default paper of the engine, which for the references is Letter.
