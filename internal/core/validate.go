package core

import (
	"fmt"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
)

// ValidateReport is the result of validating a module's schema. Schema
// errors are data of the report, not an error of the operation.
type ValidateReport struct {
	Errors   []error // from (*execmodule.Module).Validate
	Warnings []string
}

// ValidateIn resolves namespace in src and validates the module's schema,
// collecting all errors and warnings. The returned error is only a failure
// to resolve or parse the module (classified).
func ValidateIn(src discovery.Source, namespace string) (ValidateReport, error) {
	manifestPath, moduleRoot, err := src.Resolve(namespace)
	if err != nil {
		return ValidateReport{}, classify(fmt.Errorf("validate: %w", err))
	}

	// ParseModule does not validate; LoadModule would stop at the first error.
	mod, err := execmodule.ParseModule(manifestPath, moduleRoot)
	if err != nil {
		return ValidateReport{}, classify(fmt.Errorf("validate: %w", err))
	}

	errs, warnings := mod.Validate()
	return ValidateReport{Errors: errs, Warnings: warnings}, nil
}

// Validate validates namespace in the runtime's source, see ValidateIn.
func (r *Runtime) Validate(namespace string) (ValidateReport, error) {
	src, err := r.Source()
	if err != nil {
		return ValidateReport{}, err
	}
	return ValidateIn(src, namespace)
}
