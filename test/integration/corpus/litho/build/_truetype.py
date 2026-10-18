"""Minimal TrueType font programs with a caller-chosen 'cmap' subtable set.

ISO 19005-2, 6.2.11.6 tests 1 and 4 are scoped to `TrueTypeFontProgram`, so
reaching them needs a real embedded program whose 'cmap' table carries specific
(platformID, encodingID) subtables. No public font offers that on demand, and
the corpus builders take no dependency beyond pikepdf, so this module emits the
programs directly.

Each font carries three glyphs, `.notdef` plus `A` and `B`, drawn as plain
rectangles. That is enough for a viewer to render the fixture and for a
validator to resolve every code the fixture shows, which keeps the glyph
presence and width clauses quiet so the 'cmap' verdict stands alone.

Only the tables a conforming consumer needs to read a glyph are emitted: head,
hhea, maxp, hmtx, cmap, loca, glyf, name and post. Per ISO 32000-1, 9.9.2 a
font program embedded in a PDF need not be independently installable, so the
optional tables are left out.
"""

import struct

UNITS_PER_EM = 1000
GLYPH_NAMES = (".notdef", "A", "B")
ADVANCE_WIDTH = 600


def _glyf_rect(height):
    """One simple glyph: a single closed contour around a rectangle."""
    xs = (50, 550, 550, 50)
    ys = (0, 0, height, height)
    out = struct.pack(">hhhhh", 1, min(xs), min(ys), max(xs), max(ys))
    out += struct.pack(">H", len(xs) - 1)  # endPtsOfContours
    out += struct.pack(">H", 0)  # no instructions
    out += bytes([0x01] * len(xs))  # every point on-curve, 16-bit deltas
    prev = 0
    for x in xs:
        out += struct.pack(">h", x - prev)
        prev = x
    prev = 0
    for y in ys:
        out += struct.pack(">h", y - prev)
        prev = y
    if len(out) % 4:
        out += b"\0" * (4 - len(out) % 4)
    return out


def _cmap_format4(mapping):
    """A format 4 subtable covering mapping, one segment per code."""
    codes = sorted(mapping)
    segments = [(c, c, mapping[c]) for c in codes]
    segments.append((0xFFFF, 0xFFFF, 0))  # the required terminating segment
    seg_count = len(segments)

    search_range = 2
    entry_selector = 0
    while search_range * 2 <= seg_count * 2:
        search_range *= 2
        entry_selector += 1

    end_codes = b"".join(struct.pack(">H", s[1]) for s in segments)
    start_codes = b"".join(struct.pack(">H", s[0]) for s in segments)
    # idDelta maps the code straight onto the glyph id. It is a signed field
    # that consumers add modulo 65536, so it is emitted unsigned. The final
    # segment maps 0xFFFF to glyph 0, which a delta of 1 achieves by wrapping.
    deltas = b"".join(struct.pack(">H", (s[2] - s[0]) & 0xFFFF if s[2] else 1) for s in segments)
    range_offsets = b"\0\0" * seg_count

    body = struct.pack(">HHHH", seg_count * 2, search_range, entry_selector, seg_count * 2 - search_range)
    body += end_codes + b"\0\0" + start_codes + deltas + range_offsets
    return struct.pack(">HHH", 4, len(body) + 6, 0) + body


def _cmap(subtables):
    """The 'cmap' table. subtables is a list of (platformID, encodingID, map)."""
    encoded = [_cmap_format4(mapping) for _, _, mapping in subtables]
    header = struct.pack(">HH", 0, len(subtables))
    offset = 4 + 8 * len(subtables)
    records = b""
    for (platform_id, encoding_id, _), data in zip(subtables, encoded):
        records += struct.pack(">HHI", platform_id, encoding_id, offset)
        offset += len(data)
    return header + records + b"".join(encoded)


def _name():
    """A 'name' table with the four records a consumer is likely to read."""
    records = [(1, "LithoCmapProbe"), (2, "Regular"), (4, "LithoCmapProbe"), (6, "LithoCmapProbe")]
    storage = b""
    entries = b""
    for name_id, value in records:
        encoded = value.encode("utf-16-be")
        entries += struct.pack(">HHHHHH", 3, 1, 0x0409, name_id, len(encoded), len(storage))
        storage += encoded
    return struct.pack(">HHH", 0, len(records), 6 + 12 * len(records)) + entries + storage


def _post(glyph_names):
    """A 'post' table in format 2.0, which carries the glyph names.

    Format 3.0 would be shorter but stores no names, and a non-symbolic
    TrueType resolves a code through its glyph name when the 'cmap' offers no
    usable subtable (ISO 32000-1, 9.6.6.4). Without the names those lookups
    fail and the glyph presence clause fires, which would mask the 'cmap'
    verdict this module exists to isolate.
    """
    header = struct.pack(">IIhhIIIII", 0x00020000, 0, 0, 0, 0, 0, 0, 0, 0)
    # Index 0 is the standard Macintosh name `.notdef`; the rest are appended
    # as Pascal strings, which the format numbers from 258 upwards.
    indices = [0]
    names = b""
    for position, name in enumerate(glyph_names[1:]):
        indices.append(258 + position)
        names += bytes([len(name)]) + name.encode("ascii")
    body = struct.pack(">H", len(glyph_names))
    body += b"".join(struct.pack(">H", i) for i in indices)
    return header + body + names


def _checksum(data):
    padded = data + b"\0" * (-len(data) % 4)
    return sum(struct.unpack(">%dI" % (len(padded) // 4), padded)) & 0xFFFFFFFF


def build(subtables, glyph_names=GLYPH_NAMES):
    """Return the bytes of a TrueType program carrying exactly subtables.

    subtables is a list of (platformID, encodingID, {charCode: glyphName}).
    Passing several lets a fixture choose the count and the platform pairs the
    6.2.11.6 tests turn on. glyph_names renames the three glyphs in the 'post'
    table, `.notdef` first; the outlines stay the same.
    """
    assert len(glyph_names) == len(GLYPH_NAMES) and glyph_names[0] == ".notdef"
    resolved = [
        (pid, eid, {code: glyph_names.index(name) for code, name in mapping.items()})
        for pid, eid, mapping in subtables
    ]

    glyphs = [b"", _glyf_rect(700), _glyf_rect(500)]
    glyf = b"".join(glyphs)
    offsets = []
    running = 0
    for g in glyphs:
        offsets.append(running)
        running += len(g)
    offsets.append(running)
    loca = b"".join(struct.pack(">I", o) for o in offsets)  # long format

    head = struct.pack(
        ">IIIIHHQQhhhhHHhhh",
        0x00010000,  # version
        0x00010000,  # fontRevision
        0,  # checkSumAdjustment, patched below
        0x5F0F3CF5,  # magicNumber
        0b1011,  # flags
        UNITS_PER_EM,
        0,  # created
        0,  # modified
        0, -200, 550, 700,  # bounding box
        0,  # macStyle
        8,  # lowestRecPPEM
        2,  # fontDirectionHint
        1,  # indexToLocFormat: long
        0,  # glyphDataFormat
    )
    hhea = struct.pack(
        ">IhhhHhhhhhhhHHHHh",
        0x00010000, 800, -200, 0, ADVANCE_WIDTH,
        0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0,
        len(GLYPH_NAMES),
    )
    maxp = struct.pack(">IHHHHHHHHHHHHHH", 0x00010000, len(GLYPH_NAMES), 4, 1, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0)
    hmtx = b"".join(struct.pack(">Hh", ADVANCE_WIDTH, 50) for _ in GLYPH_NAMES)
    post = _post(glyph_names)

    tables = {
        b"cmap": _cmap(resolved),
        b"glyf": glyf,
        b"head": head,
        b"hhea": hhea,
        b"hmtx": hmtx,
        b"loca": loca,
        b"maxp": maxp,
        b"name": _name(),
        b"post": post,
    }

    tags = sorted(tables)
    num = len(tags)
    search_range = 16
    entry_selector = 0
    while search_range * 2 <= num * 16:
        search_range *= 2
        entry_selector += 1

    directory_size = 12 + 16 * num
    offset = directory_size
    records = b""
    body = b""
    for tag in tags:
        data = tables[tag]
        records += struct.pack(">4sIII", tag, _checksum(data), offset, len(data))
        padded = data + b"\0" * (-len(data) % 4)
        body += padded
        offset += len(padded)

    header = struct.pack(">IHHHH", 0x00010000, num, search_range, entry_selector, num * 16 - search_range)
    font = header + records + body

    # head.checkSumAdjustment is 0xB1B0AFBA less the checksum of the whole file.
    adjustment = (0xB1B0AFBA - _checksum(font)) & 0xFFFFFFFF
    head_offset = directory_size + sum(
        len(tables[t]) + (-len(tables[t]) % 4) for t in tags[: tags.index(b"head")]
    )
    return font[: head_offset + 8] + struct.pack(">I", adjustment) + font[head_offset + 12 :]


def tables_of(font):
    """Split an sfnt into {tag: bytes}, the inverse of assemble."""
    num = struct.unpack(">H", font[4:6])[0]
    out = {}
    for i in range(num):
        tag, _, off, length = struct.unpack(">4sIII", font[12 + 16 * i: 28 + 16 * i])
        out[tag] = font[off: off + length]
    return out


def assemble(version, tables):
    """Lay {tag: bytes} out as an sfnt under the 4-byte version given.

    The directory search fields are left at zero and no checkSumAdjustment is
    patched: a fixture assembled here plants a structural defect, and no
    consumer the corpus measures reads either value.
    """
    tags = sorted(tables)
    offset = 12 + 16 * len(tags)
    records = b""
    body = b""
    for tag in tags:
        data = tables[tag]
        records += struct.pack(">4sIII", tag, _checksum(data), offset, len(data))
        padded = data + b"\0" * (-len(data) % 4)
        body += padded
        offset += len(padded)
    return version + struct.pack(">HHHH", len(tags), 16, 0, 0) + records + body
