#!/usr/bin/env python3
"""List the WPT reftests the wpt corpus can score, from a WPT checkout.

Walks the directories DIRS of a web-platform-tests checkout at WPT_COMMIT and
prints, as JSON, every reftest whose test and references are plain HTML and
CSS: the files the reftest feature of test/integration/cmd/corpus converts.
A test qualifies when:

  - it links one or more references with <link rel="match"> or
    <link rel="mismatch">, and no reference links one of its own (no chains);
  - neither it nor a reference holds a script, SVG, MathML, a form control,
    an embedded document or media, an @font-face rule, a data: or remote URL,
    a vertical writing mode, a reftest-wait class, or a CSS2 flag that needs
    interaction, a user style sheet, the DOM or a font other than Ahem;
  - an XHTML file (.xht) parses alike as HTML: no CDATA section and no
    self-closed element other than a void one;
  - every stylesheet and image a file names resolves inside the directory of
    that file, so the converter reads it through a file system rooted there,
    and exists in the checkout. The one absolute URL allowed is
    /fonts/ahem.css: the corpus installs Ahem as a font instead.

The selection is deterministic: candidates are sorted by path, and a
area with more candidates than its AREA_CAP keeps the ones whose path hashes
lowest (SHA-256), so the choice is stable when other tests come and go.

Usage: list-candidates.py <wpt_checkout> > candidates.json
Run with python3 -I. Standard library only.
"""

import hashlib
import html.parser
import json
import os
import posixpath
import re
import sys

DIRS = [
    "css/CSS2/box-display",
    "css/CSS2/normal-flow",
    "css/CSS2/floats",
    "css/CSS2/floats-clear",
    "css/CSS2/margin-padding-clear",
    "css/CSS2/borders",
    "css/CSS2/text",
    "css/CSS2/lists",
    "css/CSS2/visuren",
    "css/CSS2/visudet",
    "css/CSS2/page-box",
    "css/CSS2/pagination",
    "css/CSS2/colors",
    "css/CSS2/backgrounds",
    "css/css-backgrounds",
    "css/css-text",
    "css/css-lists",
    "css/css-page",
    "css/css-break",
    "css/css-flexbox",
    "css/css-align",
    "css/css-grid",
    "css/css-multicol",
    "css/css-writing-modes",
    "css/css-overflow",
    "css/css-text-decor",
    "css/css-sizing",
]

# AREA_CAP bounds the tests one area contributes, an area being a CSS2
# chapter directory or a whole CSS module, so the large ones (css-text holds
# thousands of reftests, CSS2 backgrounds hundreds of near-identical
# background-position tests) do not crowd out the others. The caps keep the
# total within 300 to 900 tests, the size a run scores in about a minute.
AREA_CAP = {
    "css/CSS2": 24,
    "css/css-backgrounds": 80,
    "css/css-text": 120,
    "css/css-lists": 40,
    "css/css-page": 40,
    "css/css-break": 40,
    "css/css-flexbox": 200,
    "css/css-align": 20,
    "css/css-grid": 200,
    "css/css-multicol": 80,
    "css/css-writing-modes": 60,
    "css/css-overflow": 40,
    "css/css-text-decor": 40,
    "css/css-sizing": 40,
}

# AREA_FORBIDDEN are the constructs that keep a test of an area out on top
# of FORBIDDEN, for the areas of a layout mode whose tests lean on features
# the converter leaves out: grid, multi-column, intrinsic sizing keywords,
# aspect-ratio, visibility: collapse on flex items, display: contents, size
# containment, sticky positioning, zoom, the legacy -webkit-box, and the
# balanced wrapping of CSS Flexbox 2.
FLEX_FORBIDDEN = [
    re.compile(p, re.I)
    for p in [
        r"display\s*:\s*(inline-)?grid",
        r"\bgrid-",
        r"\bcolumns?(-count|-width)?\s*:",
        r"\b(min|max|fit)-content\b",
        r"\baspect-ratio\b",
        r"visibility\s*:\s*collapse",
        r"display\s*:\s*contents",
        r"\bcontain(-intrinsic-[a-z-]+)?\s*:",
        r"\bsticky\b",
        r"\bzoom\s*:",
        r"-webkit-box\b",
        r"writing-mode",
        r"\banchor",
        r"<details\b",
        r"<fieldset\b",
        r"flex-wrap\s*:\s*balance",
        r"flex-line-count",
    ]
]

# LAYOUT_FORBIDDEN keeps the tests of the wave 3 areas (grid, multi-column,
# bidirectional text, overflow, text decoration, intrinsic sizing) to what those areas add: no subgrid or
# masonry, no display: contents, aspect-ratio, containment, sticky
# positioning, zoom, anchor positioning, <details> or <fieldset>, and no
# writing mode other than horizontal-tb.
LAYOUT_FORBIDDEN = [
    re.compile(p, re.I)
    for p in [
        r"\bsubgrid\b",
        r"\bmasonry\b",
        r"\baspect-ratio\b",
        r"display\s*:\s*contents",
        r"\bcontain(-intrinsic-[a-z-]+)?\s*:",
        r"\bsticky\b",
        r"\bzoom\s*:",
        r"writing-mode\s*:\s*(?!horizontal-tb)",
        r"text-orientation",
        r"text-combine-upright",
        r"\banchor",
        r"<details\b",
        r"<fieldset\b",
        r"flex-wrap\s*:\s*balance",
        r"flex-line-count",
    ]
]

AREA_FORBIDDEN = {
    "css/css-flexbox": FLEX_FORBIDDEN,
    "css/css-align": FLEX_FORBIDDEN,
    "css/css-grid": LAYOUT_FORBIDDEN,
    "css/css-multicol": LAYOUT_FORBIDDEN,
    "css/css-writing-modes": LAYOUT_FORBIDDEN,
    "css/css-overflow": LAYOUT_FORBIDDEN,
    "css/css-text-decor": LAYOUT_FORBIDDEN,
    "css/css-sizing": LAYOUT_FORBIDDEN,
}

# AREA_REQUIRED is what every test of an area must hold: the css-align tests
# kept are the ones that align flex items.
AREA_REQUIRED = {
    "css/css-align": re.compile(r"display\s*:\s*(inline-)?flex\b", re.I),
    "css/css-writing-modes": re.compile(r"direction\s*:\s*rtl|unicode-bidi|\bdir\s*=\s*[\"']?(rtl|auto)", re.I),
}

# SKIP_DIR_NAMES are directories that hold no reftest of their own (support,
# reference, resources), tests of another kind (crashtests, parsing,
# inheritance, animations), or reftests of layout modes outside the HTML
# converter's scope (flexbox, grid, table, multicol, writing-mode).
SKIP_DIR_NAMES = {
    "support",
    "reference",
    "resources",
    "crashtests",
    "tentative",
    "animations",
    "parsing",
    "inheritance",
    "flexbox",
    "grid",
    "table",
    "multicol",
    "writing-mode",
    "subgrid",
    "masonry",
    "grid-lanes",
    "test-plan",
    "tools",
    "forms",
    "scroll-markers",
    "invalidation",
    "responsive-iframe",
    "contain-intrinsic-size",
    "aspect-ratio",
    "animation",
}

FORBIDDEN = [
    re.compile(p, re.I)
    for p in [
        r"<script\b",
        r"<svg\b",
        r"<math\b",
        r"<iframe\b",
        r"<object\b",
        r"<embed\b",
        r"<canvas\b",
        r"<video\b",
        r"<audio\b",
        r"<input\b",
        r"<button\b",
        r"<select\b",
        r"<textarea\b",
        r"<frameset\b",
        r"@font-face",
        r"\bdata:",
        r"\bjavascript:",
        r"\.svg\b",
        r"reftest-wait",
        r"vertical-(rl|lr)",
        r"sideways",
        r"\bon[a-z]+\s*=",
    ]
]

BAD_FLAGS = {"interact", "userstyle", "dom", "font", "svg", "animated", "nonHTML", "speech", "scroll"}

VOID = {"area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr"}

AHEM_CSS = "/fonts/ahem.css"

CSS_URL = re.compile(r"""url\(\s*(['"]?)([^'")]+)\1\s*\)|@import\s+(['"])([^'"]+)\3""", re.I)


class Scan(html.parser.HTMLParser):
    """Collects references, fuzzy and reftest-pages metadata, flags and resource URLs."""

    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.match, self.mismatch, self.fuzzy, self.flags = [], [], [], set()
        self.pages = ""
        self.urls, self.styles = [], []
        self.in_style = False
        self.print_class = False

    def handle_starttag(self, tag, attrs):
        a = {k.lower(): (v or "") for k, v in attrs}
        if tag == "link":
            rel = a.get("rel", "").lower().split()
            if "match" in rel:
                self.match.append(a.get("href", ""))
            elif "mismatch" in rel:
                self.mismatch.append(a.get("href", ""))
            elif "stylesheet" in rel:
                self.urls.append(a.get("href", ""))
        elif tag == "meta":
            name = a.get("name", "").lower()
            if name == "fuzzy":
                self.fuzzy.append(a.get("content", ""))
            elif name == "reftest-pages":
                self.pages = a.get("content", "").strip()
            elif name == "flags":
                self.flags.update(a.get("content", "").split())
        elif tag == "img":
            self.urls.append(a.get("src", ""))
        elif tag == "style":
            self.in_style = True
        elif tag == "html" and "reftest-print" in a.get("class", "").split():
            self.print_class = True
        if "style" in a:
            self.styles.append(a["style"])
        if "background" in a:
            self.urls.append(a["background"])

    def handle_endtag(self, tag):
        if tag == "style":
            self.in_style = False

    def handle_data(self, data):
        if self.in_style:
            self.styles.append(data)


def read(path):
    with open(path, "rb") as f:
        return f.read().decode("utf-8", "replace")


def css_urls(text):
    out = []
    for m in CSS_URL.finditer(text):
        out.append(m.group(2) or m.group(4))
    return out


def xht_parses_as_html(text):
    if "<![CDATA[" in text:
        return False
    for m in re.finditer(r"<([a-zA-Z][a-zA-Z0-9]*)\b[^<>]*/>", text):
        if m.group(1).lower() not in VOID:
            return False
    return True


def check_file(root, rel, extra=()):
    """Returns (scan, resources, reason): reason is None when the file qualifies.

    extra lists the patterns of AREA_FORBIDDEN that apply to the test's area.
    """
    path = os.path.join(root, rel)
    if not os.path.isfile(path):
        return None, [], "missing " + rel
    text = read(path)
    for pat in list(FORBIDDEN) + list(extra):
        if pat.search(text):
            return None, [], "%s: %s" % (rel, pat.pattern)
    if rel.endswith(".xht") and not xht_parses_as_html(text):
        return None, [], rel + ": XHTML only"
    scan = Scan()
    scan.feed(text)
    if scan.flags & BAD_FLAGS:
        return None, [], "%s: flags %s" % (rel, " ".join(sorted(scan.flags & BAD_FLAGS)))
    base = posixpath.dirname(rel)
    resources = []
    pending = list(scan.urls)
    for s in scan.styles:
        pending.extend(css_urls(s))
    seen = set()
    while pending:
        url = pending.pop().strip()
        if not url or url in seen or url.startswith("#"):
            continue
        seen.add(url)
        if url == AHEM_CSS:
            continue
        if url.startswith("/") or re.match(r"^[a-z][a-z0-9+.-]*:", url, re.I):
            return None, [], "%s: absolute URL %s" % (rel, url)
        url = url.split("#", 1)[0].split("?", 1)[0]
        target = posixpath.normpath(posixpath.join(base, url))
        if not (target + "/").startswith(base + "/") and base:
            return None, [], "%s: %s leaves the directory" % (rel, url)
        if not os.path.isfile(os.path.join(root, target)):
            return None, [], "%s: %s missing" % (rel, url)
        resources.append(target)
        if target.endswith(".css"):
            css = read(os.path.join(root, target))
            for pat in list(FORBIDDEN) + list(extra):
                if pat.search(css):
                    return None, [], "%s: %s" % (target, pat.pattern)
            for u in css_urls(css):
                if u.startswith("/") or re.match(r"^[a-z][a-z0-9+.-]*:", u, re.I):
                    return None, [], "%s: absolute URL %s" % (target, u)
                pending.append(posixpath.relpath(posixpath.normpath(posixpath.join(posixpath.dirname(target), u)), base))
    return scan, sorted(set(resources)), None


def is_print(rel, scan):
    stem = posixpath.splitext(posixpath.basename(rel))[0]
    return stem.endswith("-print") or "/print/" in "/" + rel or scan.print_class


def parse_fuzzy(values, ref):
    """Returns [maxDiffLo, maxDiffHi, pixelsLo, pixelsHi] for ref, or None."""
    chosen = None
    for v in values:
        v = v.strip()
        m = re.match(r"^([^:;=]+\.[a-z]+):(.*)$", v)
        if m:
            if posixpath.basename(m.group(1)) != posixpath.basename(ref):
                continue
            v = m.group(2)
        elif chosen is not None:
            continue
        parts = [p.strip() for p in v.split(";") if p.strip()]
        if len(parts) != 2:
            return "bad"
        nums = []
        for p in parts:
            p = p.split("=", 1)[-1]
            lo, _, hi = p.partition("-")
            lo = lo.strip() or "0"
            hi = hi.strip() or lo
            if not (lo.isdigit() and hi.isdigit()):
                return "bad"
            nums += [int(lo), int(hi)]
        chosen = nums
    return chosen


def resolve(test_rel, href):
    if href.startswith("/"):
        return href.lstrip("/")
    return posixpath.normpath(posixpath.join(posixpath.dirname(test_rel), href))


def candidates(root):
    out, rejected = [], {}
    for top in DIRS:
        for dirpath, dirnames, filenames in os.walk(os.path.join(root, top)):
            dirnames[:] = sorted(d for d in dirnames if d not in SKIP_DIR_NAMES)
            for name in sorted(filenames):
                if not name.endswith((".html", ".htm", ".xht")) or "-ref" in name or ".sub." in name or ".tentative." in name:
                    continue
                rel = posixpath.relpath(os.path.join(dirpath, name), root).replace(os.sep, "/")
                extra = AREA_FORBIDDEN.get(area(rel), ())
                scan, resources, reason = check_file(root, rel, extra)
                need = AREA_REQUIRED.get(area(rel))
                if reason is None and need is not None and not need.search(read(os.path.join(root, rel))):
                    reason = rel + ": no " + need.pattern
                if reason is None and not (scan.match or scan.mismatch):
                    continue
                refs = []
                if reason is None:
                    for kind, hrefs in (("match", scan.match), ("mismatch", scan.mismatch)):
                        for href in hrefs:
                            ref = resolve(rel, href)
                            rscan, rres, why = check_file(root, ref, extra)
                            if why:
                                reason = "ref " + why
                                break
                            if rscan.match or rscan.mismatch:
                                reason = "ref %s chains" % ref
                                break
                            fuzzy = parse_fuzzy(scan.fuzzy, ref)
                            if fuzzy == "bad":
                                reason = "unparsed fuzzy"
                                break
                            entry = {"rel": kind, "path": ref, "resources": rres}
                            if fuzzy:
                                entry["fuzzy"] = fuzzy
                            refs.append(entry)
                        if reason:
                            break
                if reason:
                    rejected[reason.split(":")[-1].strip()] = rejected.get(reason.split(":")[-1].strip(), 0) + 1
                    continue
                entry = {
                    "id": rel,
                    "print": is_print(rel, scan),
                    "resources": resources,
                    "refs": refs,
                }
                if scan.pages:
                    # WPT's reftest-pages: the test's pages compared, in
                    # the order listed; the reference's are all compared.
                    entry["pages"] = scan.pages
                out.append(entry)
    return out, rejected


def area(test_id):
    """Returns the area of a test: css/CSS2/<chapter> or css/<module>."""
    parts = test_id.split("/")
    if parts[1] == "CSS2":
        return "/".join(parts[:3])
    return "/".join(parts[:2])


def cap(tests):
    by_area = {}
    for t in tests:
        by_area.setdefault(area(t["id"]), []).append(t)
    kept = []
    for a in sorted(by_area):
        limit = AREA_CAP["css/CSS2"] if a.startswith("css/CSS2/") else AREA_CAP[a]
        group = sorted(by_area[a], key=lambda t: hashlib.sha256(t["id"].encode()).hexdigest())[:limit]
        kept.extend(group)
    return sorted(kept, key=lambda t: t["id"])


def main():
    if len(sys.argv) != 2:
        print(__doc__.strip().splitlines()[-2], file=sys.stderr)
        sys.exit(2)
    tests, rejected = candidates(sys.argv[1])
    kept = cap(tests)
    print("%d candidates, %d kept after the per-area caps" % (len(tests), len(kept)), file=sys.stderr)
    for reason, n in sorted(rejected.items(), key=lambda kv: -kv[1])[:25]:
        print("  rejected %5d  %s" % (n, reason), file=sys.stderr)
    json.dump(kept, sys.stdout, indent=2, sort_keys=True)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
