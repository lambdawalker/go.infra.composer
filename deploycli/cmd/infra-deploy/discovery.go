package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/lambdawalker/go.infra.composer/discovery"
)

type discoverySpec struct {
	APIURL, CognitoIssuer, CognitoUserPoolID, CognitoClientID string
	ProbeMethod, ProbePath                                    string
	ProbeStatus                                               int
}

func (s discoverySpec) validate(outputs []string) error {
	if s.APIURL == "" || !slices.Contains(outputs, s.APIURL) {
		return errors.New("discovery APIURL must name an allowlisted output")
	}
	cognito := []string{s.CognitoIssuer, s.CognitoUserPoolID, s.CognitoClientID}
	if slices.ContainsFunc(cognito, func(name string) bool { return name != "" }) {
		for _, name := range cognito {
			if name == "" || !slices.Contains(outputs, name) {
				return errors.New("discovery Cognito configuration must name three allowlisted outputs")
			}
		}
	}
	route, err := url.ParseRequestURI(s.ProbePath)
	if err != nil || route.Host != "" || route.IsAbs() || !strings.HasPrefix(s.ProbePath, "/") ||
		strings.HasPrefix(s.ProbePath, "//") || route.RawQuery != "" || strings.ContainsAny(s.ProbePath, "?#") ||
		(s.ProbeMethod != http.MethodGet && s.ProbeMethod != http.MethodPost) ||
		(s.ProbeStatus != http.StatusOK && s.ProbeStatus != http.StatusNoContent && s.ProbeStatus != http.StatusUnauthorized) {
		return errors.New("discovery must declare a GET or POST readiness path and a supported response status")
	}
	return nil
}

func (s discoverySpec) fromOutputs(raw []byte) (discovery.Configuration, error) {
	var outputs map[string]json.RawMessage
	if err := json.Unmarshal(raw, &outputs); err != nil {
		return discovery.Configuration{}, errors.New("invalid discovery outputs")
	}
	var result discovery.Configuration
	for _, output := range []struct {
		key   string
		field *string
	}{
		{s.APIURL, &result.APIURL}, {s.CognitoIssuer, &result.CognitoIssuer},
		{s.CognitoUserPoolID, &result.CognitoUserPoolID}, {s.CognitoClientID, &result.CognitoClientID},
	} {
		if output.key != "" && (json.Unmarshal(outputs[output.key], output.field) != nil || *output.field == "") {
			return discovery.Configuration{}, fmt.Errorf("missing public discovery output %s", output.key)
		}
	}
	return result, result.Validate()
}

type discoveryTarget struct {
	URL, Region string
}

func (t discoveryTarget) validate() error {
	u, err := url.Parse(t.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		u.EscapedPath() != discovery.Path || strings.ContainsAny(t.URL, "?#") || !validRegion(t.Region) {
		return errors.New("discovery requires an HTTPS /v1/environments URL and AWS region")
	}
	return nil
}

type discoveryClient struct {
	discoveryTarget
	environment string
	credentials aws.CredentialsProvider
	http        *http.Client
}

func openDiscovery(ctx context.Context, private, environment, endpoint, discoveryRegion, stackRegion string) (*discoveryClient, error) {
	path := filepath.Join(private, "discovery.json")
	var target discoveryTarget
	body, err := os.ReadFile(path)
	if err == nil {
		if json.Unmarshal(body, &target) != nil || target.validate() != nil {
			return nil, errors.New("invalid saved discovery target")
		}
		if (endpoint != "" && endpoint != target.URL) || (discoveryRegion != "" && discoveryRegion != target.Region) {
			return nil, errors.New("discovery target differs from this environment's saved index")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	} else if endpoint == "" {
		return nil, nil
	} else {
		target = discoveryTarget{URL: endpoint, Region: discoveryRegion}
		if target.Region == "" {
			target.Region = stackRegion
		}
		if err := target.validate(); err != nil {
			return nil, err
		}
	}
	if !discovery.ValidEnvironment(environment) {
		return nil, errors.New("invalid discovery environment")
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(target.Region))
	if err != nil {
		return nil, errors.New("could not load AWS configuration for discovery")
	}
	client := &discoveryClient{discoveryTarget: target, environment: environment, credentials: cfg.Credentials,
		http: &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
	return client, nil
}

func (c *discoveryClient) begin(ctx context.Context, private string) (int64, error) {
	revision, err := c.change(ctx, discovery.Change{Operation: "begin"})
	if err != nil {
		return 0, err
	}
	raw, err := json.Marshal(c.discoveryTarget)
	if err != nil {
		return 0, err
	}
	if err := atomicWrite(filepath.Join(private, "discovery.json"), raw); err != nil {
		return 0, err
	}
	return revision, nil
}

func (c *discoveryClient) change(ctx context.Context, change discovery.Change) (int64, error) {
	raw, err := json.Marshal(change)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/"+c.environment+"/changes", bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	credentials, err := c.credentials.Retrieve(ctx)
	if err != nil {
		return 0, errors.New("could not load AWS credentials for discovery")
	}
	hash := sha256.Sum256(raw)
	if err := v4.NewSigner().SignHTTP(ctx, credentials, req, hex.EncodeToString(hash[:]), "execute-api", c.Region, time.Now()); err != nil {
		return 0, errors.New("could not sign discovery update")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("discovery %s request failed", change.Operation)
	}
	defer res.Body.Close()
	switch {
	case change.Operation == "begin" && res.StatusCode == http.StatusOK:
		var receipt discovery.Receipt
		if json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&receipt) == nil && receipt.Revision > 0 {
			return receipt.Revision, nil
		}
	case change.Operation != "begin" && res.StatusCode == http.StatusNoContent:
		return 0, nil
	case res.StatusCode == http.StatusConflict:
		return 0, discovery.ErrSuperseded
	}
	return 0, fmt.Errorf("discovery %s was rejected (HTTP %d)", change.Operation, res.StatusCode)
}

func (s discoverySpec) ready(ctx context.Context, client *http.Client, connection discovery.Configuration) error {
	ctx, stop := context.WithTimeout(ctx, time.Minute)
	defer stop()
	address := strings.TrimSuffix(connection.APIURL, "/") + s.ProbePath
	for {
		req, err := http.NewRequestWithContext(ctx, s.ProbeMethod, address, nil)
		if err != nil {
			return err
		}
		res, err := client.Do(req)
		if err == nil {
			res.Body.Close()
			if res.StatusCode == s.ProbeStatus {
				return nil
			}
		}
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.New("discovery readiness check did not reach the expected response")
		case <-timer.C:
		}
	}
}
