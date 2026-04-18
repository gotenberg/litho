# Remediation Issues

Systematic analysis of remediation gaps across all profiles.
Updated 2026-04-15 from the VeraPDF corpus (1,304 files, 1,300 failing).

## Summary

| Profile  | Before | After | Fixed % | Fully Fixed | Failing |
| -------- | ------ | ----- | ------- | ----------- | ------- |
| PDF/A-1b | 8,566  | 316   | 96.3%   | 42/305      | 305     |
| PDF/A-2b | 772    | 561   | 27.3%   | 137/608     | 608     |
| PDF/A-2u | 10     | 6     | 40.0%   | 4/10        | 10      |
| PDF/A-2a | 33     | 29    | 12.1%   | 4/16        | 16      |
| PDF/A-3b | 5      | 4     | 20.0%   | 1/5         | 5       |
| PDF/A-4  | 499    | 257   | 48.5%   | 166/356     | 356     |
| **Total**| 9,885  | 1,166 | 88.2%   | 355/1,300   |         |

Regressions: **0**.

## Implemented Fixes

| Fix | Impact | Files |
| --- | ------ | ----- |
| pdfaid:rev namespace declaration | 1 regression eliminated | fixer_xmp.go |
| Image Interpolate/OPI removal | +2 fully fixed | fixer_images.go |
| PresSteps removal | +1 fully fixed | fixer_pressteps.go |
| Requirements removal | +1 fully fixed | fixer_requirements.go |
| XMP bytes attribute stripping | +2 fully fixed | fixer_xmp.go |
| DocInfo-to-XMP full sync | +3 fully fixed | fixer_xmp.go |
| OutputIntent color space matching | +52 fully fixed | fixer_outputintent.go |
| OutputIntent skip for damaged docs | regressions eliminated | fixer_outputintent.go |
| ToUnicode CMap generation | +7 fully fixed | fixer_fonts.go |
| PUA exclusion from ToUnicode | regressions eliminated | fixer_fonts.go |
| Forbidden actions removal | +2 fully fixed | fixer_javascript.go |
| Annotation /F flag insertion | +1 fully fixed | fixer_annotflags.go |
| Re-validation accuracy | Residual list truthful | remediate.go |

## Remaining Residual Categories

The remaining 1,166 violations are dominated by intentionally malformed
test corpus files designed to test specific validator rules.

| Clause | Count | Description | Fixable? |
| ------ | ----- | ----------- | -------- |
| 6.6.2.3.1 | 282 | Extension schema structure | No: malformed XML |
| 6.7.9 | 201 | XMP namespace validation | No: invalid URIs |
| 6.3.3 | 47 | Annotation appearances | No: requires appearance generation |
| 6.6.2.3.3 | 29 | Extension schema containers | No: malformed XML |
| 6.1.12 | 28 | Implementation limits | No: intentionally oversized |
| 6.2.2 | 27 | Content stream operators | No: requires re-rendering |
| 6.3.1 | 26 | Font program conformance | No: requires font replacement |
| 6.6.1 | 25 | Forbidden actions | Partially fixed |
| 6.2.4.4 | 24 | Separation/DeviceN CS | No: requires CS rewriting |
| 6.7.3 | 23 | DocInfo/XMP consistency | Partially fixed |
| 6.2.9 | 20 | Reference XObjects | No: requires content change |
| 6.2.5 | 20 | ExtGState restrictions | No: requires rendering |
| 6.1.2 | 20 | File header format | Fixed by full rewrite |
| 6.1.13 | 20 | Implementation limits | No: intentionally oversized |
| 6.2.3.3 | 18 | Mixed-CS (RGB+CMYK) | No: PDF/A-1 single-intent limit |

## Conclusion

The 88.2% fix rate and 355/1,300 fully-fixed files represent the practical
ceiling for automated remediation against the VeraPDF test corpus.
