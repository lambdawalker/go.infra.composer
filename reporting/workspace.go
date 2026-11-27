package reporting

import (
	"encoding/base64"
	"html/template"
	"io/fs"
	"path"
	"strings"

	"github.com/lambdawalker/go.infra.composer/internal/reportui"
)

// Ship each service family's artwork once, including alternate models the
// reader may select. Paths always come from the embedded catalog.
func snapshotArtwork(artifacts []Artifact) (map[string]string, error) {
	families := map[string]bool{}
	for _, a := range artifacts {
		if a.Component {
			families["pulumi/component"] = true
			continue
		}
		provider, rest, _ := strings.Cut(a.Type, ":")
		service := strings.FieldsFunc(rest, func(r rune) bool { return r == '/' || r == ':' })
		if len(service) == 0 {
			continue
		}
		switch provider {
		case "cloudflare":
			families["cloudflare/dns"] = true
		case "random":
			families["random/random"] = true
		case "aws":
			name := service[0]
			switch name {
			case "apigateway", "apigatewayv2":
				name = "api"
			case "cloudwatch":
				families["aws/eventbridge"] = true
			case "secretsmanager":
				families["aws/secret"] = true
			}
			families["aws/"+name] = true
		}
	}
	files := reportui.Files()
	out := make(map[string]string)
	for _, pattern := range []string{"icons/*/*.svg", "models/*/*.glb"} {
		paths, err := fs.Glob(files, pattern)
		if err != nil {
			return nil, err
		}
		for _, name := range paths {
			_, relative, _ := strings.Cut(name, "/")
			stem, _, _ := strings.Cut(strings.TrimSuffix(relative, path.Ext(relative)), "-")
			if !families[stem] {
				continue
			}
			data, err := files.ReadFile(name)
			if err != nil {
				return nil, err
			}
			out["/"+name] = base64.StdEncoding.EncodeToString(data)
		}
	}
	return out, nil
}

// Only compile-time assets enter trusted template contexts. Snapshot fields
// continue through html/template's contextual escaping.
func staticText(files fs.FS, name string) string {
	data, err := fs.ReadFile(files, name)
	if err != nil {
		panic(err)
	}
	return string(data)
}

var workspaceStyle = template.CSS(staticText(reportui.UI, "theme.css") + staticText(reportui.UI, "topology.css") + staticText(reportTemplates, "report.css"))
var workspaceRuntime = template.JS("/*\n" + staticText(reportui.UI, "vendor/three-LICENSE.txt") + "\n*/\n" + staticText(reportui.UI, "vendor/topology-three.js") + "\n" + staticText(reportui.Files(), "aws-icons.js") + "\n" + staticText(reportui.UI, "topology.js"))
