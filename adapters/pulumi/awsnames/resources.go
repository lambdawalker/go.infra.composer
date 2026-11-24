package awsnames

import (
	"fmt"

	names "github.com/lambdawalker/go.infra.composer/awsnames"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// ValidationError identifies an invalid logical name before provider registration.
type ValidationError struct {
	err error
}

func (e *ValidationError) Error() string { return e.err.Error() }

// NewAutoNamed checks the logical name used as the base for the provider's
// default auto-name. Use this for constructors that omit an explicit name.
func NewAutoNamed[A, R any](ctx *pulumi.Context, kind names.Kind, register func(*pulumi.Context, string, *A, ...pulumi.ResourceOption) (*R, error), logicalName string, args *A, opts ...pulumi.ResourceOption) (*R, error) {
	if err := names.Validate(kind, logicalName); err != nil {
		return nil, &ValidationError{err}
	}
	return register(ctx, logicalName, args, opts...)
}

// Input checks a physical name and preserves a computed value's Pulumi
// dependencies. Unknown values are checked when they become available.
func Input(kind names.Kind, value pulumi.StringInput) (pulumi.StringInput, error) {
	if value == nil {
		return nil, fmt.Errorf("%s is required", kind)
	}
	if literal, ok := value.(pulumi.String); ok {
		if err := names.Validate(kind, string(literal)); err != nil {
			return nil, err
		}
		return value, nil
	}
	return value.ToStringOutput().ApplyT(func(name string) (string, error) {
		if err := names.Validate(kind, name); err != nil {
			return "", err
		}
		return name, nil
	}).(pulumi.StringOutput), nil
}
