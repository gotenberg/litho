# Built-in ICC profiles

`CGATS001Compat-v2-micro.icc` is the profile litho reads DeviceCMYK in when neither the document nor the caller names one. It comes from [Compact ICC Profiles](https://github.com/saucecontrol/Compact-ICC-Profiles) at revision `bdd84663061bc4ae95ca70decff54f581e27f702`, the file PDF.js ships for the same purpose.

It is dedicated to the public domain under [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/): see [LICENSE](LICENSE).

`srgb.icc` is the profile litho states sRGB with: remediation embeds it for the RGB output intents and transparency blending spaces it writes, and redaction for the images it rewrites in sRGB. It is a version 2.1 display (`mntr`) RGB profile whose header names LittleCMS (`lcms`) as its creator. It has shipped with litho since `00e32798`, and its provenance is not otherwise recorded.
