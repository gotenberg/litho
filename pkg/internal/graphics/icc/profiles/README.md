# Built-in ICC profiles

`CGATS001Compat-v2-micro.icc` is the profile litho reads DeviceCMYK in when neither the document nor the caller names one. It comes from [Compact ICC Profiles](https://github.com/saucecontrol/Compact-ICC-Profiles) at revision `bdd84663061bc4ae95ca70decff54f581e27f702`, the file PDF.js ships for the same purpose.

It is dedicated to the public domain under [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/): see [LICENSE](LICENSE).

`srgb.icc` is the profile litho states sRGB with: remediation embeds it for the RGB output intents and transparency blending spaces it writes, and redaction for the images it rewrites in sRGB. It is a version 2.1 display (`mntr`) RGB profile whose header names LittleCMS (`lcms`) as its creator. It has shipped with litho since `00e32798`, and its provenance is not otherwise recorded.

`sgray-v2.icc` is the stock grey profile remediation writes for a grey output intent whose destination profile it replaces. It is a version 2.4 monochrome display (`mntr`) profile that [cmd/gen-iccv2](../../../../../cmd/gen-iccv2) computes from IEC 61966-2-1: the sRGB transfer function sampled at 1024 points as its grey tone curve, and the sRGB D65 white as its media white point. A grey level reads through it as the sRGB colour of three equal components. It copies no vendor profile; regenerate it with `go generate ./pkg/internal/graphics/icc/...`, and a test fails when the committed file and the generator differ.
