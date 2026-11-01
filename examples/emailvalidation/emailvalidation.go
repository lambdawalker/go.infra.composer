// Package emailvalidation declares a validator without importing authentication.
package emailvalidation

import (
	composer "github.com/lambdawalker/go.infra.composer"
	"github.com/lambdawalker/go.infra.composer/examples/contracts"
)

func Feature() *composer.Feature {
	fallback := &composer.Feature{ID: "email-test-users", Source: "emailvalidation standalone default", Resources: []composer.Resource{{ID: "pool", Kind: "example:user-pool", Outputs: map[string]composer.Field{"id": {Type: composer.String}}}}, Offerings: []composer.Offering{{ID: "users", Contract: contracts.UserPool(), Values: map[string]composer.Value{"id": composer.ResourceRef("email-test-users.pool", "id")}, TestOnly: true}}}
	return &composer.Feature{
		ID: "emailvalidation", Source: "examples/emailvalidation",
		Requirements: []composer.Requirement{{ID: "users", Contract: contracts.UserPool(), Default: &composer.Default{Feature: fallback, Offering: "users"}}},
		Resources:    []composer.Resource{{ID: "validator", Kind: "example:email-validator", Inputs: map[string]composer.Value{"userPool": composer.RequirementRef("emailvalidation", "users", "id")}, Outputs: map[string]composer.Field{"endpoint": {Type: composer.String}}}},
		Offerings:    []composer.Offering{{ID: "validator", Contract: contracts.EmailValidator(), Values: map[string]composer.Value{"endpoint": composer.ResourceRef("emailvalidation.validator", "endpoint")}}},
	}
}
