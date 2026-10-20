#!/usr/bin/env python3
"""Build sharedarray_2b.pdf: pages whose direct /Contents arrays share a
stream, one of them carrying an invalid rendering intent.

ISO 32000-1, 7.7.3.3 lets /Contents be an array of streams, and nothing
keeps two pages from naming the same stream in theirs. Aggressive remediation
replaces the invalid intent (ISO 19005-2, 6.2.6) by rewriting the page's
content. Written in place, into the array's first stream with the others
emptied, the rewrite takes a later shared stream off the other page and,
through a shared first stream, paints this page's content on it.

Each page paints one square of its own and the shared square:

| Page | /Contents  | Streams                                             | Answers    |
| ---- | ---------- | --------------------------------------------------- | ---------- |
| 1    | `[A S]`    | A: `/Bogus ri`, a square at 10; S: the shared square | 6.2.6 t1   |
| 2    | `[B S]`    | B: a square at 60                                    | nothing    |
| 3    | `[T C]`    | T: the shared square; C: `/Bogus ri`, a square at 10 | 6.2.6 t1   |
| 4    | `[T D]`    | D: a square at 60                                    | nothing    |
| 5    | `[E]`      | E: `/Bogus ri`, a square at 10, shared by no page    | 6.2.6 t1   |

Page 2 must keep the shared square after page 1 is repaired, page 4 must not
gain page 3's square, and page 5 is the control the in-place rewrite keeps.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()


def stream(ops):
    return pdf.make_indirect(Stream(pdf, ops.encode("ascii") + b"\n"))


BAD = "/Bogus ri 0 g 10 10 30 30 re f"
OWN = "0 g 60 10 30 30 re f"
SHARED = "0.5 g 10 60 80 30 re f"


def page(*contents):
    d = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 100, 100]),
        Resources=Dictionary(),
        Contents=Array(list(contents)),
    )
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(d)))


s = stream(SHARED)
page(stream(BAD), s)
page(stream(OWN), s)

t = stream(SHARED)
page(t, stream(BAD))
page(t, stream(OWN))

page(stream(BAD))

# Deliberately no /Metadata and no output intent.
save(pdf, "sharedarray_2b.pdf")
