package reporting

import (
	"strings"
)

// Only structural AWS relationships belong in the hierarchy. A function that
// uses a table, role or signing key is still their peer in the component.
var attachmentHosts = map[string]string{
	"lambda:permission":            "lambda:function",
	"iam:rolepolicy":               "iam:role",
	"iam:rolepolicyattachment":     "iam:role",
	"apigatewayv2:stage":           "apigatewayv2:api",
	"apigatewayv2:authorizer":      "apigatewayv2:api",
	"apigatewayv2:integration":     "apigatewayv2:api",
	"apigatewayv2:route":           "apigatewayv2:api",
	"apigatewayv2:apimapping":      "apigatewayv2:domainname",
	"cognito:userpoolclient":       "cognito:userpool",
	"cloudwatch:eventtarget":       "cloudwatch:eventrule",
	"secretsmanager:secretversion": "secretsmanager:secret",
	"acm:certificatevalidation":    "acm:certificate",
	"kms:alias":                    "kms:key",
}

func awsKind(typ string) string {
	parts := strings.Split(strings.ToLower(typ), ":")
	if len(parts) != 3 || parts[0] != "aws" {
		return ""
	}
	service, _, _ := strings.Cut(parts[1], "/")
	return service + ":" + parts[2]
}

// ResolveAttachments preserves the recorded Pulumi parent and derives a presentation edge
// from typed dependencies or the project's explicit hints for older inventories.
func ResolveAttachments(items []Artifact, hints map[string]string) map[string]string {
	byKey := make(map[string]Artifact, len(items))
	byName := make(map[string][]string)
	for _, a := range items {
		byKey[a.Key] = a
		byName[a.Name] = append(byName[a.Name], a.Key)
	}
	resolved := make(map[string]string)
	for _, a := range items {
		hostType, ok := attachmentHosts[awsKind(a.Type)]
		if !ok || a.Component {
			continue
		}
		parent, hasParent := byKey[a.Parent]
		if hasParent && !parent.Component {
			if awsKind(parent.Type) == hostType {
				resolved[a.Key] = parent.Key
			}
			continue
		}
		hosts := make(map[string]bool)
		for _, key := range a.Dependencies {
			if host, ok := byKey[key]; ok && !host.Component && awsKind(host.Type) == hostType {
				hosts[key] = true
			}
		}
		if hint := hints[a.Name]; len(hosts) == 0 && hint != "" {
			for _, key := range byName[hint] {
				host := byKey[key]
				if !host.Component && awsKind(host.Type) == hostType && host.Parent == a.Parent {
					hosts[key] = true
				}
			}
		}
		if len(hosts) == 1 {
			for key := range hosts {
				if byKey[key].Parent == a.Parent {
					resolved[a.Key] = key
				}
			}
		}
	}
	return resolved
}
