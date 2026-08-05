"""JP2 containers around a fixed JPEG 2000 codestream.

ISO 19005-2, 6.2.8.3 states five tests over the JPEG 2000 data of a `JPXDecode`
image, and every one of them reads a box of the JP2 container rather than the
codestream: the channel count and bit depth from `ihdr` and `bpcc`, the colour
specification from `colr`. This module writes those boxes, so a fixture plants
each defect by naming a value rather than by re-encoding an image.

The codestreams are embedded. A JPEG 2000 codestream is entropy coded, which no
builder writes by hand, and the corpus builders take no dependency beyond
pikepdf. They were produced once with OpenJPEG:

    python3 -c "open('raw2.raw','wb').write(bytes([50]*64 + [200]*64))"
    opj_compress -i raw2.raw -o two.j2k -F 8,8,2,8,u -n 1 -r 1

and the three-component one from a 8x8x3 raw of the same shape. The optional
`COM` marker OpenJPEG writes carries its own version string, so it is stripped:
the bytes here do not change when the encoder is upgraded.

Both are 8x8 at 8 bits per component. `TWO` has two components, which no
conforming file may have and which no ordinary encoder emits, and `THREE` has
three.
"""

import base64
import struct

TWO = base64.b64decode(
    "/0//UQAsAAAAAAAIAAAACAAAAAAAAAAAAAAACAAAAAgAAAAAAAAAAAACBwEBBwEB/1IA"
    "DAAAAAEAAAQEAAH/XAAEQED/kAAKAAAAAAA5AAH/k8+0TBFQVKNlXbAAAwkJfXzMAADC"
    "Qj/PtEgUAFyjZV2wAAMJCNUKGEhL/3//2Q=="
)

THREE = base64.b64decode(
    "/0//UQAvAAAAAAAIAAAACAAAAAAAAAAAAAAACAAAAAgAAAAAAAAAAAADBwEBBwEBBwEB"
    "/1IADAAAAAEBAAQEAAH/XAAEQED/kAAKAAAAAAA+AAH/k8PnGBFQVK+CPUgAAYSEv8fU"
    "GBFQVKJ9tpAAAwkJf8+0PBQAXK+FDRQAAAAYSEv/f//Z"
)

SIZE = 8

# ISO/IEC 15444-1, Table I.9: an enumerated colour space of 16 is sRGB and 19
# is CIEJab, which ISO 19005-2, 6.2.8.3 test 4 forbids by name.
ENUM_SRGB = 16
ENUM_CIEJAB = 19


def _box(tag, payload):
    """One JP2 box: a 32-bit length covering the header, the type, the payload."""
    return struct.pack(">I", 8 + len(payload)) + tag + payload


def ihdr(components, depth):
    """An image header box. Depth is stored one less than its value."""
    return _box(b"ihdr", struct.pack(">IIHBBBB", SIZE, SIZE, components, depth - 1, 7, 0, 0))


def colr(meth, approx, enum_cs=None):
    """One colour specification box, ISO/IEC 15444-1, I.5.3.3.

    METH 1 names an enumerated colour space, METH 2 embeds an ICC profile. Any
    other value is what ISO 19005-2, 6.2.8.3 test 3 rejects, and the box then
    carries no colour data at all.
    """
    payload = bytes([meth, 0, approx])
    if meth == 1:
        payload += struct.pack(">I", enum_cs)
    return _box(b"colr", payload)


def bpcc(depths):
    """A bits-per-component box, one byte per component, each one less."""
    return _box(b"bpcc", bytes(d - 1 for d in depths))


def container(codestream, components, depth, colrs, depths=None):
    """A JP2 file: signature, file type, the header superbox, the codestream."""
    header = ihdr(components, depth)
    if depths is not None:
        header += bpcc(depths)
    for spec in colrs:
        header += spec
    signature = struct.pack(">I", 12) + b"jP  " + b"\x0d\x0a\x87\x0a"
    file_type = _box(b"ftyp", b"jp2 " + struct.pack(">I", 0) + b"jp2 ")
    return signature + file_type + _box(b"jp2h", header) + _box(b"jp2c", codestream)
