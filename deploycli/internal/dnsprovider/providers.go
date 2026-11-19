// Package dnsprovider defines the DNS hosts supported by the deployment wizard.
package dnsprovider

import (
	"fmt"
	"slices"
	"strings"
)

type Provider struct {
	ID       string
	Name     string
	Guidance string
	aliases  []string
}

// Add a provider's definition in its own file, then register it here.
// The order determines the numbers displayed in the wizard.
var providers = []Provider{route53, cloudflare}

func All() []Provider { return slices.Clone(providers) }

func Choices() string {
	choices := make([]string, 0, len(providers))
	for i, provider := range providers {
		choices = append(choices, fmt.Sprintf("%d. %s", i+1, provider.Name))
	}
	return strings.Join(choices, ", ")
}

func Lookup(input string) (Provider, bool) {
	input = strings.TrimSpace(input)
	for i, provider := range providers {
		if strings.EqualFold(input, provider.ID) || strings.EqualFold(input, provider.Name) || input == fmt.Sprint(i+1) {
			return provider, true
		}
		for _, alias := range provider.aliases {
			if strings.EqualFold(input, alias) {
				return provider, true
			}
		}
	}
	return Provider{}, false
}
