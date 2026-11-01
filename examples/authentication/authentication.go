// Package authentication declares authentication without importing its consumers.
package authentication

import (
	composer "github.com/lambdawalker/go.infra.composer"
	"github.com/lambdawalker/go.infra.composer/examples/contracts"
)

func Feature() *composer.Feature {
	fallback := &composer.Feature{ID: "auth-test-validator", Source: "authentication standalone default", Resources: []composer.Resource{{ID: "validator", Kind: "example:email-validator", Outputs: map[string]composer.Field{"endpoint": {Type: composer.String}}}}, Offerings: []composer.Offering{{ID: "validator", Contract: contracts.EmailValidator(), Values: map[string]composer.Value{"endpoint": composer.ResourceRef("auth-test-validator.validator", "endpoint")}, TestOnly: true}}}
	return &composer.Feature{
		ID: "authentication", Source: "examples/authentication",
		Resources:    []composer.Resource{{ID: "pool", Kind: "example:user-pool", Inputs: map[string]composer.Value{"name": composer.Literal("example-users")}, Outputs: map[string]composer.Field{"id": {Type: composer.String}}}},
		Offerings:    []composer.Offering{{ID: "users", Contract: contracts.UserPool(), Values: map[string]composer.Value{"id": composer.ResourceRef("authentication.pool", "id")}}},
		Requirements: []composer.Requirement{{ID: "email-validator", Contract: contracts.EmailValidator(), Default: &composer.Default{Feature: fallback, Offering: "validator"}}},
		Runtime:      []composer.RuntimeUse{{Requirement: "email-validator", Reason: "validate email when users register", Source: "examples/authentication"}},
	}
}
