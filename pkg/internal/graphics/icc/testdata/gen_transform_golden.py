"""Record littleCMS's conversions between the ICC profiles the engine tests.

Writes transform_golden.json beside this script: for each pair of profiles,
rendering intent and black point compensation setting, the 8-bit colours
littleCMS (through Pillow's ImageCms) converts a grid of 8-bit inputs to.
link_test.go compares litho's Link against it. Run it through
`make icc-goldens`, which installs the Pillow release versions.env pins.
"""

import itertools
import json
import os

import PIL
from PIL import Image, ImageCms

HERE = os.path.dirname(os.path.abspath(__file__))
PROFILES = os.path.join(HERE, "..", "profiles")

# Each profile: its file (None for the Lab profile littleCMS builds) and the
# Pillow mode of its data colour space.
SOURCES = {
    "srgb": (os.path.join(PROFILES, "srgb.icc"), "RGB"),
    "sgray": (os.path.join(PROFILES, "sgray-v2.icc"), "L"),
    "cgats": (os.path.join(PROFILES, "cgats001-cmyk-v2.icc"), "CMYK"),
    "micro": (os.path.join(PROFILES, "CGATS001Compat-v2-micro.icc"), "CMYK"),
    "v4cmyk": (os.path.join(HERE, "v4-cmyk-lut.icc"), "CMYK"),
    "v4rgb": (os.path.join(HERE, "v4-rgb-display.icc"), "RGB"),
    "v4xyz": (os.path.join(HERE, "v4-rgb-lut-xyz.icc"), "RGB"),
    "lab": (None, "LAB"),
}

PAIRS = [
    ("srgb", "cgats"),
    ("cgats", "srgb"),
    ("micro", "srgb"),
    ("micro", "cgats"),
    ("cgats", "sgray"),
    ("sgray", "cgats"),
    ("srgb", "sgray"),
    ("sgray", "srgb"),
    ("srgb", "v4cmyk"),
    ("v4cmyk", "srgb"),
    ("cgats", "v4cmyk"),
    ("v4cmyk", "cgats"),
    ("v4rgb", "cgats"),
    ("cgats", "v4rgb"),
    ("v4xyz", "srgb"),
    ("srgb", "v4xyz"),
    ("lab", "cgats"),
    ("lab", "srgb"),
    ("srgb", "lab"),
]

# The levels each channel of the input grid takes.
LEVELS = {
    "L": list(range(0, 256, 15)),
    "RGB": [0, 64, 128, 192, 255],
    "LAB": [0, 64, 128, 192, 255],
    "CMYK": [0, 128, 255],
}

CHANNELS = {"L": 1, "RGB": 3, "LAB": 3, "CMYK": 4}


def profile(name):
    path, _ = SOURCES[name]
    if path is None:
        return ImageCms.createProfile("LAB")
    return ImageCms.getOpenProfile(path)


def grid(mode):
    return [list(p) for p in itertools.product(LEVELS[mode], repeat=CHANNELS[mode])]


def convert(src, dst, intent, bpc):
    in_mode, out_mode = SOURCES[src][1], SOURCES[dst][1]
    flags = ImageCms.Flags.NOOPTIMIZE
    if bpc:
        flags |= ImageCms.Flags.BLACKPOINTCOMPENSATION
    transform = ImageCms.buildTransform(
        profile(src), profile(dst), in_mode, out_mode, ImageCms.Intent(intent), flags
    )
    inputs = grid(in_mode)
    image = Image.frombytes(
        in_mode, (len(inputs), 1), bytes(v for px in inputs for v in px)
    )
    out = ImageCms.applyTransform(image, transform).tobytes()
    n = CHANNELS[out_mode]
    return [list(out[i : i + n]) for i in range(0, len(out), n)]


def main():
    cases = []
    for src, dst in PAIRS:
        for intent in range(4):
            for bpc in (False, True):
                if intent == 3 and bpc:
                    continue
                cases.append(
                    {
                        "src": src,
                        "dst": dst,
                        "intent": intent,
                        "bpc": bpc,
                        "outputs": convert(src, dst, intent, bpc),
                    }
                )
    golden = {
        "pillow": PIL.__version__,
        "littlecms": ImageCms.core.littlecms_version,
        "levels": LEVELS,
        "cases": cases,
    }
    with open(os.path.join(HERE, "transform_golden.json"), "w") as f:
        json.dump(golden, f)
        f.write("\n")


if __name__ == "__main__":
    main()
