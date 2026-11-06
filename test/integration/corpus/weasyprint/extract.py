#!/usr/bin/env python3
"""Extract the WeasyPrint draw tests into the weasyprint corpus fixtures.

Reads tests/draw/test_*.py of an unpacked WeasyPrint release and writes, for
every assert_pixels(expected, html) call whose two arguments are literal
strings, one JSON fixture: the test name, the HTML, the expected pixel rows,
and the colour each pixel character stands for (PIXELS_BY_CHAR of
tests/draw/__init__.py, read from the release, not restated here).

An argument is literal when it is a string constant, possibly implicitly
concatenated, or a name bound to one: a module-level assignment, or an
assignment in the test function itself that is never rebound. Anything else
(an f-string, a %-format, a parameter of a parametrized test, a call) is
computed, and the call is skipped and counted. A test that calls
assert_pixels more than once yields one fixture per call, suffixed _2, _3.
Tests WeasyPrint marks xfail keep the mark in their fixture.

The script also copies the files of tests/resources the fixtures name (the
images and stylesheets their HTML refers to) and the test UA stylesheet into
the corpus resources directory, and the release's LICENSE beside it. The
WeasyPrint test font goes to a font directory outside git instead, its
SHA-256 printed for comparison with the pin in versions.env.

Usage: extract.py <weasyprint_tree> <fixtures_dir> <resources_dir> <fonts_dir>
Run with python3 -I. Standard library only.
"""

import ast
import hashlib
import json
import os
import shutil
import sys

# MAX_RESOURCE_BYTES caps the size of a resource copied into git: 256 KiB.
# Only the CMYK profile and the JPEG embedding it exceed it, and the fixtures
# naming them, colour-managed images, are skipped and counted.
MAX_RESOURCE_BYTES = 256 * 1024

# FONT_FILES are the test font's files: weasyprint.otf is the face the test
# UA stylesheet declares, the others are the same glyphs in other formats.
FONT_FILES = {"weasyprint.otf", "weasyprint.otb", "weasyprint.woff"}


def literal(node, names):
    """Returns the string node evaluates to, or None when it is computed."""
    if isinstance(node, ast.Constant) and isinstance(node.value, str):
        return node.value
    if isinstance(node, ast.Name):
        return names.get(node.id)
    return None


def string_bindings(body):
    """Maps each name a block binds exactly once, to a string constant, to it."""
    counts, values = {}, {}
    for stmt in body:
        for node in ast.walk(stmt) if not isinstance(stmt, (ast.FunctionDef, ast.ClassDef)) else [stmt]:
            if isinstance(node, (ast.Assign, ast.AugAssign, ast.AnnAssign)):
                targets = node.targets if isinstance(node, ast.Assign) else [node.target]
                for t in targets:
                    if isinstance(t, ast.Name):
                        counts[t.id] = counts.get(t.id, 0) + 1
                        if isinstance(node, ast.Assign) and isinstance(node.value, ast.Constant) and isinstance(node.value.value, str):
                            values[t.id] = node.value.value
    return {k: v for k, v in values.items() if counts.get(k) == 1}


def parameters(func):
    """Returns the names a test function takes, which pytest may parametrize."""
    args = func.args
    return {a.arg for a in args.args + args.kwonlyargs + args.posonlyargs}


def is_xfail(func):
    for d in func.decorator_list:
        target = d.func if isinstance(d, ast.Call) else d
        if isinstance(target, ast.Attribute) and target.attr == "xfail":
            return True
    return False


def pixel_map(draw_init):
    """Reads PIXELS_BY_CHAR from tests/draw/__init__.py."""
    tree = ast.parse(open(draw_init, encoding="utf-8").read())
    for node in tree.body:
        if isinstance(node, ast.Assign) and any(isinstance(t, ast.Name) and t.id == "PIXELS_BY_CHAR" for t in node.targets):
            value = ast.literal_eval(node.value)
            return {k: (list(v) if v is not None else None) for k, v in sorted(value.items())}
    raise SystemExit("PIXELS_BY_CHAR not found in " + draw_init)


def rows(expected):
    """Parses an expected grid as tests/draw/__init__.py parse_pixels does."""
    lines = (line.split("#")[0].strip() for line in expected.splitlines())
    return [line for line in lines if line]


def main():
    if len(sys.argv) != 5:
        print("usage: extract.py <weasyprint_tree> <fixtures_dir> <resources_dir> <fonts_dir>", file=sys.stderr)
        sys.exit(2)
    tree, fixtures_dir, resources_dir, fonts_dir = sys.argv[1:]
    draw = os.path.join(tree, "tests", "draw")
    pixels = pixel_map(os.path.join(draw, "__init__.py"))

    if os.path.isdir(fixtures_dir):
        shutil.rmtree(fixtures_dir)
    os.makedirs(fixtures_dir)
    written, skipped, bad_grid = 0, [], []
    for name in sorted(os.listdir(draw)):
        if not (name.startswith("test_") and name.endswith(".py")):
            continue
        module = ast.parse(open(os.path.join(draw, name), encoding="utf-8").read())
        module_names = string_bindings(module.body)
        for func in module.body:
            if not isinstance(func, ast.FunctionDef) or not func.name.startswith("test_"):
                continue
            local = dict(module_names)
            local.update(string_bindings(func.body))
            for p in parameters(func):
                local.pop(p, None)
            calls = [
                n for n in ast.walk(func)
                if isinstance(n, ast.Call) and isinstance(n.func, ast.Name) and n.func.id == "assert_pixels"
            ]
            calls.sort(key=lambda n: (n.lineno, n.col_offset))
            for i, call in enumerate(calls, start=1):
                test_id = "%s__%s" % (name[:-3], func.name) + ("" if i == 1 else "_%d" % i)
                args = call.args
                expected = literal(args[0], local) if len(args) == 2 else None
                html = literal(args[1], local) if len(args) == 2 else None
                if expected is None or html is None or call.keywords:
                    skipped.append(test_id)
                    continue
                grid = rows(expected)
                if not grid or len({len(r) for r in grid}) != 1 or any(c not in pixels for r in grid for c in r):
                    bad_grid.append(test_id)
                    continue
                fixture = {
                    "name": test_id,
                    "source": "tests/draw/%s::%s" % (name, func.name),
                    "xfail": is_xfail(func),
                    "html": html,
                    "expected": grid,
                    "pixels": pixels,
                }
                with open(os.path.join(fixtures_dir, test_id + ".json"), "w", encoding="utf-8") as f:
                    json.dump(fixture, f, indent=2, sort_keys=True, ensure_ascii=False)
                    f.write("\n")
                written += 1

    src = os.path.join(tree, "tests", "resources")
    resources = {}
    for dirpath, _, filenames in os.walk(src):
        for fn in filenames:
            rel = os.path.relpath(os.path.join(dirpath, fn), src).replace(os.sep, "/")
            resources[rel] = os.path.join(dirpath, fn)
    too_large = {rel for rel, path in resources.items() if os.path.getsize(path) > MAX_RESOURCE_BYTES}
    used, oversized = set(), []
    for fn in sorted(os.listdir(fixtures_dir)):
        path = os.path.join(fixtures_dir, fn)
        html = json.load(open(path, encoding="utf-8"))["html"]
        named = {rel for rel in resources if rel in html and rel not in FONT_FILES}
        if named & too_large:
            oversized.append(fn[:-5])
            os.remove(path)
            written -= 1
            continue
        used |= named

    if os.path.isdir(resources_dir):
        shutil.rmtree(resources_dir)
    os.makedirs(resources_dir)
    for rel in sorted(used | {"tests_ua.css"}):
        dst = os.path.join(resources_dir, rel)
        os.makedirs(os.path.dirname(dst), exist_ok=True)
        shutil.copyfile(resources[rel], dst)
    shutil.copyfile(os.path.join(tree, "LICENSE"), os.path.join(os.path.dirname(os.path.abspath(resources_dir)), "LICENSE"))
    os.makedirs(fonts_dir, exist_ok=True)
    font = os.path.join(fonts_dir, "weasyprint.otf")
    shutil.copyfile(resources["weasyprint.otf"], font)
    digest = hashlib.sha256(open(font, "rb").read()).hexdigest()
    print("  weasyprint: font %s, sha256 %s" % (font, digest))

    print("  weasyprint: %d fixtures written, %d calls skipped (computed grid or HTML), "
          "%d naming a resource over %d bytes, %d with an unparsable grid"
          % (written, len(skipped), len(oversized), MAX_RESOURCE_BYTES, len(bad_grid)))
    for t in skipped:
        print("    skipped " + t)
    for t in oversized:
        print("    oversized resource " + t)
    for t in bad_grid:
        print("    bad grid " + t)


if __name__ == "__main__":
    main()
