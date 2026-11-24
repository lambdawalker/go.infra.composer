package awsnames

import (
	"errors"
	"testing"

	names "github.com/lambdawalker/go.infra.composer/awsnames"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestNewAutoNamedPreservesValidNamesAndRejectsInvalidOnes(t *testing.T) {
	args := new(struct{})
	registered := ""
	register := func(_ *pulumi.Context, name string, given *struct{}, _ ...pulumi.ResourceOption) (*string, error) {
		if given != args {
			t.Fatal("constructor arguments changed")
		}
		registered = name
		return &registered, nil
	}
	_, err := NewAutoNamed(nil, names.LambdaFunction, register, "testhost.app-lambda", args)
	var invalid *ValidationError
	if !errors.As(err, &invalid) || registered != "" {
		t.Fatalf("invalid name reached constructor: %q, %v", registered, err)
	}
	result, err := NewAutoNamed(nil, names.DynamoDBTable, register, "testhost.app-table", args)
	if err != nil || result == nil || *result != "testhost.app-table" {
		t.Fatalf("valid name not preserved: %v, %v", result, err)
	}
}
