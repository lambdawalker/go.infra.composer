package wizardweb

import (
	"reflect"
	"testing"

	"github.com/lambdawalker/go.infra.composer/reporting"
)

func TestAttachmentsPreserveOwnershipAndExcludeOrdinaryDependencies(t *testing.T) {
	items := []reporting.Artifact{
		{Key: "host", Component: true}, {Key: "other", Component: true},
		{Key: "role", Name: "role", Type: "aws:iam/role:Role", Parent: "host"},
		{Key: "fn", Name: "function", Type: "aws:lambda/function:Function", Parent: "host", Dependencies: []string{"role", "table"}},
		{Key: "table", Type: "aws:dynamodb/table:Table", Parent: "other"},
		{Key: "perm", Type: "aws:lambda/permission:Permission", Parent: "host", Dependencies: []string{"fn", "table"}},
		{Key: "policy", Name: "legacy-policy", Type: "aws:iam/rolePolicy:RolePolicy", Parent: "host"},
		{Key: "cross", Type: "aws:lambda/permission:Permission", Parent: "other", Dependencies: []string{"fn"}},
		{Key: "foreign", Type: "custom:lambda:Permission", Parent: "host", Dependencies: []string{"fn"}},
	}
	got := resolveAttachments(items, map[string]string{"legacy-policy": "role"})
	if !reflect.DeepEqual(got, map[string]string{"perm": "fn", "policy": "role"}) || items[4].Parent != "other" {
		t.Fatal(got)
	}
}
