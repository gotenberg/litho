# Built-in ICC profiles

`CGATS001Compat-v2-micro.icc` is the profile litho reads DeviceCMYK in when neither the document nor the caller names one. It comes from [Compact ICC Profiles](https://github.com/saucecontrol/Compact-ICC-Profiles) at revision `bdd84663061bc4ae95ca70decff54f581e27f702`, the file PDF.js ships for the same purpose.

It is dedicated to the public domain under [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/): see [LICENSE](LICENSE).

`srgb.icc` is the profile litho states sRGB with: remediation embeds it for the RGB output intents and transparency blending spaces it writes, and redaction for the images it rewrites in sRGB. It is a version 2.1 display (`mntr`) RGB profile whose header names LittleCMS (`lcms`) as its creator. It has shipped with litho since `00e32798`, and its provenance is not otherwise recorded.

`sgray-v2.icc` is the stock grey profile remediation writes for a grey output intent whose destination profile it replaces. It is a version 2.4 monochrome display (`mntr`) profile that [cmd/gen-iccv2](../../../../../cmd/gen-iccv2) computes from IEC 61966-2-1: the sRGB transfer function sampled at 1024 points as its grey tone curve, and the sRGB D65 white as its media white point. A grey level reads through it as the sRGB colour of three equal components. It copies no vendor profile; regenerate it with `go generate ./pkg/internal/graphics/icc/...`, and a test fails when the committed file and the generator differ.

It is dedicated to the public domain under [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/), the dedication `CGATS001Compat-v2-micro.icc` carries: see [LICENSE](LICENSE). Its copyright (`cprt`) tag states the dedication, so the licence travels with every copy remediation embeds.

`cgats001-cmyk-v2.icc` is the stock CMYK printer profile, for a writer that needs a version 2 output profile of the CMYK data colour space. It is a version 2.4 output (`prtr`) profile over the Lab connection space that [cmd/gen-iccv2](../../../../../cmd/gen-iccv2) builds from `CGATS001Compat-v2-micro.icc`. Its three `AToB` tags share that profile's `A2B0` table byte for byte, so a CMYK colour reads through it exactly as DeviceCMYK reads by default. Its three `BToA` tags share one 17-point Lab table the generator inverts from that characterisation by least squares, within the 300 per cent ink limit of CGATS TR 001 and with a medium grey-component replacement, clipping an out-of-gamut colour to the nearest printable one. Its `gamt` tag marks the Lab nodes the inversion misses by more than 1 ΔE\*ab. It copies no vendor profile; regenerate it with `go generate ./pkg/internal/graphics/icc/...`, and a test fails when the committed file and the generator differ.

It is dedicated to the public domain under [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/), as the characterisation it is built from is: see [LICENSE](LICENSE). Its `cprt` tag states the dedication and names that source.
