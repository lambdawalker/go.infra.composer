// Package awsnames validates AWS resource names used by Pulumi factories.
package awsnames

import (
	"fmt"
	"regexp"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

var lambdaName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// LambdaFunctionName checks the name for creating an AWS Lambda function.
// With no explicit name, it checks the logical name as the base of Pulumi's
// default auto-name and returns nil to keep provider auto-naming enabled.
// Explicit Pulumi outputs are checked when known and retain their dependencies.
func LambdaFunctionName(logicalName string, name pulumi.StringInput) (pulumi.StringInput, error) {
	if name == nil {
		if !lambdaName.MatchString(logicalName) {
			return nil, fmt.Errorf("Lambda %q: the Pulumi auto-name base must contain 1–64 letters, digits, hyphens, or underscores", logicalName)
		}
		return nil, nil
	}

	validate := func(value string) (string, error) {
		if !lambdaName.MatchString(value) {
			return "", fmt.Errorf("Lambda %q: the function name must contain 1–64 letters, digits, hyphens, or underscores", logicalName)
		}
		return value, nil
	}
	if literal, ok := name.(pulumi.String); ok {
		if _, err := validate(string(literal)); err != nil {
			return nil, err
		}
		return name, nil
	}
	return name.ToStringOutput().ApplyT(validate).(pulumi.StringOutput), nil
}
