package pdf

// buildValidationErrors evaluates the compatibility matrix against the
// pipeline's declared stages and returns one [*PipelineStageError] per
// violated rule. The rules are documented in the Pipeline section of the
// package README.
func (p *Pipeline) buildValidationErrors() []error {
	var errs []error

	if len(p.stages) == 0 {
		errs = append(errs, &PipelineStageError{
			Index: 0,
			Name:  "pipeline",
			Err:   ErrPipelineEmpty,
		})
		return errs
	}

	// Structural flags accumulated while walking the stages.
	var (
		hasSource       bool
		hasFlatten      bool
		hasEncrypt      bool
		hasPDFA         bool
		signIdx         = -1
		firstPDFAIdx    = -1
		firstEncryptIdx = -1
	)

	for i, s := range p.stages {
		if s.kind() == kindOpen {
			if i != 0 {
				// Open is only valid as the first stage.
				errs = append(errs, &PipelineStageError{
					Index: i,
					Name:  s.name(),
					Err:   ErrPipelineNoSource,
				})
			}
			hasSource = true
		} else if !hasSource {
			errs = append(errs, &PipelineStageError{
				Index: i,
				Name:  s.name(),
				Err:   ErrPipelineNoSource,
			})
		}

		switch st := s.(type) {
		case *splitStage:
			if len(st.pages) == 0 {
				errs = append(errs, &PipelineStageError{
					Index: i,
					Name:  s.name(),
					Err:   ErrPipelineEmptyPageList,
				})
			}
			// A restructure resets the form-flatten flag because the new
			// document can have its own form state.
			hasFlatten = false
		case *mergeStage:
			if len(st.others) == 0 {
				errs = append(errs, &PipelineStageError{
					Index: i,
					Name:  s.name(),
					Err:   ErrPipelineNothingToMerge,
				})
			}
			hasFlatten = false
		case *flattenFormStage:
			hasFlatten = true
		case *setFormFieldStage:
			if hasFlatten {
				errs = append(errs, &PipelineStageError{
					Index: i,
					Name:  s.name(),
					Err:   ErrPipelineFormAfterFlatten,
				})
			}
		case *encryptStage:
			hasEncrypt = true
			if firstEncryptIdx == -1 {
				firstEncryptIdx = i
			}
			if hasPDFA {
				errs = append(errs, &PipelineStageError{
					Index: i,
					Name:  s.name(),
					Err:   ErrPipelineEncryptWithPDFA,
				})
			}
		case *remediatePDFAStage:
			hasPDFA = true
			if firstPDFAIdx == -1 {
				firstPDFAIdx = i
			}
			if hasEncrypt {
				errs = append(errs, &PipelineStageError{
					Index: i,
					Name:  s.name(),
					Err:   ErrPipelineEncryptWithPDFA,
				})
			}
		case *signStage:
			signIdx = i
		}
	}

	if signIdx >= 0 && signIdx != len(p.stages)-1 {
		errs = append(errs, &PipelineStageError{
			Index: signIdx,
			Name:  "sign",
			Err:   ErrPipelineSignNotLast,
		})
	}
	if signIdx >= 0 && hasEncrypt {
		errs = append(errs, &PipelineStageError{
			Index: signIdx,
			Name:  "sign",
			Err:   ErrPipelineSignWithEncrypt,
		})
	}

	return errs
}
