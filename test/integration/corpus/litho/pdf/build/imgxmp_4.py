#!/usr/bin/env python3
"""Build imgxmp_4.pdf: image XObject dictionaries and XMP packets for PDF/A-4.

Two ISO 19005-4 clusters were unreached by the rest of the corpus. 6.2.7.1
governs the image XObject dictionary, and `complex_4.pdf` touches only its
`/Interpolate` test. 6.7.2.1 governs the XMP packet, and nothing reached
anything past the "catalog has a metadata stream" test.

Both clusters need sibling objects rather than one loaded object.

The four XMP tests all scope to veraPDF's `XMPPackage`, so a single malformed
packet would cascade: a packet that is not valid UTF-8 does not parse either,
which masks the header-attribute tests and takes the identification schema down
with it. A PDF may carry a `/Metadata` stream on the catalog and on every page,
and veraPDF models each one as its own `XMPPackage`, so each defect gets its own
packet on its own page. That isolation is the point of the fixture. The catalog
packet stays conforming, because the document has to keep declaring PDF/A-4.

Page-level packets are also the only packets a pikepdf builder can plant a
header defect in. `Pdf.save` reserialises the catalog packet through its own XMP
writer, which repairs a `bytes=` attribute, an `encoding=` attribute, broken
markup and a UTF-16 encoding alike. Page-level metadata streams pass through
untouched.

The images follow the same rule: four sibling XObjects, one defect each, so a
check that reports the first offending image and stops reports one rule where
four are due. Every image uses a CalRGB colour space and every image mask is
painted through an explicit CalGray fill colour, so ISO 19005-4, 6.2.4.3 stays
out of the way of the tests this fixture is about. The file declares no font for
the same reason.

`ImMask` is one image mask reached two ways: drawn directly and named as the
`/Mask` of `ImBase`. veraPDF only reads `/BitsPerComponent` on an image mask it
reaches through another image's `/Mask`, and litho only reads it on an image
mask drawn from the page resources, so one object serving both roles carries the
single defect both models can see.

Two tests of the two clauses stay out. `6.2.7.1/3` is `/Interpolate`, which
`complex_4.pdf` already covers and which every image here carries as false, so
it is a control rather than a gap. `6.7.2.1/1` needs the catalog to have no
metadata stream, which would take the PDF/A-4 declaration with it and silence
everything else the fixture is for.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import zlib

from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    attach_xmp,
    pikepdf,
    save,
    xmp_packet,
)

pdf = pikepdf.new()

# The CIE D65 white point, in the CIE-based colour spaces of ISO 32000-2, 8.6.5.
# Both keep the images and the image masks clear of the device colour space
# tests, which have their own fixtures.
WHITE_POINT = Array([0.9505, 1.0, 1.089])
CAL_RGB = Array([Name.CalRGB, Dictionary(WhitePoint=WHITE_POINT)])
CAL_GRAY = Array([Name.CalGray, Dictionary(WhitePoint=WHITE_POINT)])

# 2x2 CalRGB pixels. The bytes are shared by every image below: none of the
# defects is in the sample data.
PIXELS = zlib.compress(bytes([255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 0]))

# 2x2 stencil, one bit per sample, rows padded to a byte.
STENCIL = b"\x80\x00"


def image(**extra):
    """Build a 2x2 CalRGB image XObject that conforms unless extra breaks it."""
    img = Stream(pdf, PIXELS)
    img.Type = Name.XObject
    img.Subtype = Name.Image
    img.Width = 2
    img.Height = 2
    img.ColorSpace = CAL_RGB
    img.BitsPerComponent = 8
    img.Filter = Name.FlateDecode
    # ISO 19005-4, 6.2.7.1 t3 allows the key when its value is false. Setting it
    # explicitly on every image keeps that test a control rather than a gap.
    img.Interpolate = False
    for key, value in extra.items():
        setattr(img, key, value)
    return pdf.make_indirect(img)


def image_mask(bits):
    """Build a 2x2 stencil mask declaring the given /BitsPerComponent."""
    img = Stream(pdf, STENCIL)
    img.Type = Name.XObject
    img.Subtype = Name.Image
    img.Width = 2
    img.Height = 2
    img.ImageMask = True
    img.BitsPerComponent = bits
    img.Decode = Array([0, 1])
    img.Interpolate = False
    return pdf.make_indirect(img)


# t1: an /Alternates array. Its one alternate conforms in every respect, so the
# report belongs to the image that carries the key, not to what it points at.
im_alt = image(
    Alternates=Array([Dictionary(Image=image(), DefaultForPrinting=False)])
)

# t2: an /OPI dictionary, the open prepress interface proxy for an image held
# outside the file. ISO 19005-4, 6.2.7.1 forbids the key outright.
opi = Dictionary()
opi["/2.0"] = Dictionary(
    Type=Name.OPI,
    Version=2.0,
    F=String("scan.tif"),
    Size=Array([2, 2]),
    CropRect=Array([0, 2, 2, 0]),
)
im_opi = image(OPI=opi)

# t4: /BitsPerComponent 3, which is in no row of ISO 32000-2, Table 87.
im_bpc = image(BitsPerComponent=3)

# t5: an image mask declaring 2 bits per sample where only 1 is defined. It is
# drawn from the page resources and named as the /Mask of ImBase, because the
# two validators reach an image mask by different routes. 2 is a legal value for
# a regular image, so the defect stays inside t5 rather than also failing t4.
im_mask = image_mask(2)
im_base = image(Mask=im_mask)

# Controls. A conforming image and a conforming image mask stand beside the
# four defects, so a check that condemns every image once it has seen one bad
# one reports them too.
im_ok = image()
im_mask_ok = image_mask(1)

resources = Dictionary(
    ColorSpace=Dictionary(CsGray=CAL_GRAY),
    XObject=Dictionary(
        ImAlt=im_alt,
        ImOPI=im_opi,
        ImBPC=im_bpc,
        ImMask=im_mask,
        ImBase=im_base,
        ImOK=im_ok,
        ImMaskOK=im_mask_ok,
    ),
)

# An image mask paints the current fill colour through its samples, so the fill
# colour has to be calibrated too or the mask drags 6.2.4.3 in with it.
image_page_content = b"""/CsGray cs 0 sc
q 40 0 0 40 40 500 cm /ImAlt Do Q
q 40 0 0 40 100 500 cm /ImOPI Do Q
q 40 0 0 40 160 500 cm /ImBPC Do Q
q 40 0 0 40 220 500 cm /ImMask Do Q
q 40 0 0 40 280 500 cm /ImBase Do Q
q 40 0 0 40 340 500 cm /ImOK Do Q
q 40 0 0 40 400 500 cm /ImMaskOK Do Q
"""

# Packet header variants. veraPDF reads the attributes straight out of the
# <?xpacket ...?> processing instruction, so the defect is in the header alone
# and the packet body stays well-formed and parseable.
PACKET_ID = 'id="W5M0MpCehiHzreSzNTczkc9d"'
HEADER_OK = '<?xpacket begin="﻿" %s?>' % PACKET_ID
HEADER_BYTES = '<?xpacket begin="﻿" %s bytes="1024"?>' % PACKET_ID
HEADER_ENCODING = '<?xpacket begin="﻿" %s encoding="UTF-8"?>' % PACKET_ID
TRAILER = '<?xpacket end="w"?>'

BODY = """<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">%s</rdf:li></rdf:Alt></dc:title>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>"""

# A start tag whose end tag differs in case. XML is case sensitive, so the
# packet is not well-formed and has no valid serialization.
BODY_MALFORMED = """<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">
   <dc:title>page metadata</dc:Title>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>"""


def page_packet(header=HEADER_OK, body=None, encoding="utf-8"):
    """Serialise one page-level XMP packet.

    No page packet carries a pdfaid property: the identification schema belongs
    to the catalog packet, which is the only one veraPDF treats as the main XMP
    package.
    """
    text = "%s\n%s\n%s\n" % (header, body if body else BODY % "page metadata", TRAILER)
    if encoding == "utf-16":
        return "﻿".encode("utf-16-be") + text.encode("utf-16-be")
    return text.encode("utf-8")


def metadata_stream(packet):
    """Wrap an XMP packet in a metadata stream dictionary."""
    stream = Stream(pdf, packet)
    stream.Type = Name.Metadata
    stream.Subtype = Name.XML
    return pdf.make_indirect(stream)


# One page per defect. Page 1 carries the images and a conforming packet, so its
# silence proves the four below report per packet rather than once per document.
pages = [
    (image_page_content, page_packet(), resources),
    (b"", page_packet(header=HEADER_BYTES), Dictionary()),
    (b"", page_packet(header=HEADER_ENCODING), Dictionary()),
    (b"", page_packet(body=BODY_MALFORMED), Dictionary()),
    (b"", page_packet(encoding="utf-16"), Dictionary()),
]

for content, packet, page_resources in pages:
    page = Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=page_resources,
        Contents=Stream(pdf, content),
        Metadata=metadata_stream(packet),
    )
    pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

# ISO 19005-4, 6.7.3 requires pdfaid:part 4 and pdfaid:rev 2020. Both are
# correct here: the catalog packet is a control, not a defect.
attach_xmp(pdf, xmp_packet(part=4, rev=2020))

save(pdf, "imgxmp_4.pdf", min_version="2.0")
