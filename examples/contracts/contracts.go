// Package contracts contains the neutral contracts shared by independent examples.
package contracts

import composer "github.com/lambdawalker/go.infra.composer"

func UserPool() composer.Contract {
	return composer.Contract{ID: "example.user-pool", Major: 1, Fields: map[string]composer.Field{"id": {Type: composer.String}}}
}

func EmailValidator() composer.Contract {
	return composer.Contract{ID: "example.email-validator", Major: 1, Fields: map[string]composer.Field{"endpoint": {Type: composer.String}}}
}
