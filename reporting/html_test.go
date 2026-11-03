package reporting

import (
	"bytes"
	"strings"
	"testing"
)

func TestHTMLBrowserLayoutAndEscapedMetadata(t *testing.T) {
	r := Report{Scope: Scope{Application: "attestra", Repository: "lambdawalker/go.attestra.aws"}, Artifacts: []Artifact{
		{Key: "bucket", Name: "Evidence", Owner: "go.feature", Feature: "capture", Type: "aws:s3/bucket:Bucket", Priority: 20, Observation: Observation{Status: Present}},
		{Key: "object", Name: `<img src=x onerror=alert(1)>`, Parent: "bucket", Owner: "go.feature", Feature: "capture", Type: "aws:s3/bucketObject:BucketObject", Observation: Observation{Status: Unknown}},
	}}
	var out bytes.Buffer
	if err := WriteHTML(&out, r); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{`aria-label="Deployment overview"`, `Application / Feature`, `github.com/lambdawalker/go.feature`, `class="owner-short">go.feature`, `class="owner-full"><strong>Owner</strong>`, `status-present`, `status-unknown`, `#icon-database`, `#icon-file`, `value="priority"`, `value="owner"`, `value="type"`, `Subartifacts · 1`} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Count(html, `<td>capture</td>`) != 1 {
		t.Fatal("duplicate feature rows")
	}
	if strings.Contains(html, `<img src=x`) || strings.Contains(html, `<details open`) || strings.Contains(html, `<script src=`) {
		t.Fatal("unsafe or non-self-contained output")
	}
	if !strings.Contains(html, `data-name="&lt;img`) {
		t.Fatal("attribute not escaped")
	}
}
func TestOwnerRepositoryAndIcons(t *testing.T) {
	for _, c := range []struct{ owner, repo, want string }{
		{"go.feature", "org/app", "github.com/org/go.feature"},
		{"different/feature", "org/app", "github.com/different/feature"},
		{"github.com/org/feature", "org/app", "github.com/org/feature"},
		{"go.feature", "", "go.feature"},
		{"", "org/app", "Not specified"},
	} {
		if got := ownerRepository(c.owner, c.repo); got != c.want {
			t.Errorf("%q: %q", c.owner, got)
		}
	}
	for _, c := range []struct{ typ, icon string }{{"aws:lambda/function:Function", "function"}, {"cloudflare:dnsRecord", "globe"}, {"aws:iam/rolePolicy:RolePolicy", "shield"}, {"unknown", "box"}} {
		if got := artifactIcon(Artifact{Type: c.typ}); got != c.icon {
			t.Errorf("%s: %s", c.typ, got)
		}
	}
}
