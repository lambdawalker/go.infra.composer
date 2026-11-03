package reporting

import (
	"embed"
	"html/template"
	"io"
	"sort"
	"strings"
)

//go:embed report.html
var reportTemplates embed.FS

type repositoryRow struct{ Name, Repository string }

// Bare owner names inherit only the explicitly supplied application repository's
// organization; fully qualified ownership is preserved, never replaced.
func ownerRepository(owner, repository string) string {
	owner = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(owner), "https://"), "github.com/")
	repository = strings.TrimPrefix(strings.TrimPrefix(repository, "https://"), "github.com/")
	if owner == "" {
		return "Not specified"
	}
	if !strings.Contains(owner, "/") {
		if parts := strings.Split(repository, "/"); len(parts) == 2 {
			owner = parts[0] + "/" + owner
		}
	}
	if strings.Count(owner, "/") == 1 {
		return "github.com/" + owner
	}
	return owner
}
func shortOwner(owner string) string {
	owner = strings.TrimSuffix(owner, "/")
	if i := strings.LastIndex(owner, "/"); i >= 0 {
		return owner[i+1:]
	}
	if owner == "" {
		return "Owner not specified"
	}
	return owner
}
func featureRows(r Report) []repositoryRow {
	seen := map[repositoryRow]bool{}
	for _, a := range r.Artifacts {
		if a.Feature != "" {
			seen[repositoryRow{a.Feature, ownerRepository(a.Owner, r.Scope.Repository)}] = true
		}
	}
	rows := make([]repositoryRow, 0, len(seen))
	for row := range seen {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].Repository < rows[j].Repository
	})
	return rows
}
func artifactIcon(a Artifact) string {
	t := strings.ToLower(a.Type)
	switch {
	case strings.Contains(t, "bucketobject"), strings.Contains(t, "objectv2"):
		return "file"
	case strings.Contains(t, "dns"), strings.Contains(t, "domain"), strings.Contains(t, "distribution"):
		return "globe"
	case strings.Contains(t, "permission"), strings.Contains(t, "policy"), strings.Contains(t, "role"), strings.Contains(t, "secret"), strings.Contains(t, "certificate"), strings.Contains(t, "userpool"):
		return "shield"
	case strings.Contains(t, "function"):
		return "function"
	case strings.Contains(t, "table"), strings.Contains(t, "bucket"):
		return "database"
	case strings.Contains(t, "queue"), strings.Contains(t, "topic"):
		return "queue"
	case strings.Contains(t, "route"), strings.Contains(t, "integration"), strings.Contains(t, "api"):
		return "network"
	case strings.Contains(t, "environment"):
		return "environment"
	case strings.Contains(t, "log"), strings.Contains(t, "alarm"):
		return "activity"
	default:
		return "box"
	}
}

var page = template.Must(template.New("report.html").Funcs(template.FuncMap{
	"fields": artifactFields, "display": display, "shortOwner": shortOwner,
	"ownerRepository": ownerRepository, "icon": artifactIcon,
	"dict": func(nodes []Node, repository string) any {
		return struct {
			Nodes      []Node
			Repository string
		}{nodes, repository}
	},
}).ParseFS(reportTemplates, "report.html"))

func WriteHTML(w io.Writer, r Report) error {
	r = Normalize(r)
	return page.Execute(w, struct {
		Report
		Nodes    []Node
		Context  []field
		Summary  string
		Issues   []Artifact
		Features []repositoryRow
	}{r, Hierarchy(r.Artifacts), contextFields(r)[2:], summary(r), issues(r), featureRows(r)})
}
