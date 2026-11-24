package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/lambdawalker/go.infra.composer/discovery"
)

func TestDiscoveryPublishesOnlyMappedOutputs(t *testing.T) {
	spec := discoverySpec{
		APIURL: "mobileURL", CognitoIssuer: "issuer", CognitoUserPoolID: "pool", CognitoClientID: "client",
		ProbeMethod: "POST", ProbePath: "/v1/verification/status", ProbeStatus: http.StatusUnauthorized,
	}
	outputs := []string{"mobileURL", "issuer", "pool", "client"}
	if err := spec.validate(outputs); err != nil {
		t.Fatal(err)
	}
	if err := spec.validate(outputs[:3]); err == nil {
		t.Fatal("accepted an output outside the public allowlist")
	}
	entry, err := spec.fromOutputs([]byte(`{
		"mobileURL":"https://dev.components.example", "issuer":"https://issuer.example/pool",
		"pool":"us-east-2_demo", "client":"demo-client", "secret":"private-key"
	}`))
	if err != nil || entry.APIURL != "https://dev.components.example" {
		t.Fatalf("map discovery outputs: %+v %v", entry, err)
	}
	raw, err := json.Marshal(entry)
	if err != nil || strings.Contains(string(raw), "private-key") {
		t.Fatalf("unexpected discovery output: %s %v", raw, err)
	}
}

func TestDiscoverySignedLifecycle(t *testing.T) {
	var operations []string
	var mu sync.Mutex
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != discovery.Path+"/dev/changes" || !strings.Contains(r.Header.Get("Authorization"), "/us-east-2/execute-api/aws4_request") {
			t.Error("unexpected discovery route or request signature")
		}
		var c discovery.Change
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			t.Error(err)
		}
		mu.Lock()
		operations = append(operations, c.Operation)
		mu.Unlock()
		if c.Operation == "begin" {
			fmt.Fprint(w, `{"revision":17}`)
			return
		}
		if c.Revision != 17 {
			t.Error("update did not use the reserved revision")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := discoveryClient{
		discoveryTarget: discoveryTarget{URL: server.URL + discovery.Path, Region: "us-east-2"},
		environment:     "dev", credentials: credentials.NewStaticCredentialsProvider("fixture", "fixture", ""), http: server.Client(),
	}
	ctx := t.Context()
	revision, err := client.begin(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []discovery.Change{
		{Operation: "publish", Revision: revision, Configuration: &discovery.Configuration{APIURL: "https://dev.example"}},
		{Operation: "retire", Revision: revision},
	} {
		if _, err := client.change(ctx, change); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(operations, ",") != "begin,publish,retire" {
		t.Fatalf("unexpected operations %v", operations)
	}
}
