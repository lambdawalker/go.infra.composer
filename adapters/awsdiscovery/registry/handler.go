package registry

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/lambdawalker/go.infra.composer/discovery"
)

// Handle adapts the API Gateway v2 payload and its verified IAM identity.
func Handle(ctx context.Context, event events.APIGatewayV2HTTPRequest, store discovery.Store) events.APIGatewayV2HTTPResponse {
	if event.IsBase64Encoded {
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusBadRequest}
	}
	authorizer := event.RequestContext.Authorizer
	canWrite := authorizer != nil && authorizer.IAM != nil && authorizer.IAM.UserARN != ""
	req, err := http.NewRequestWithContext(ctx, event.RequestContext.HTTP.Method, event.RawPath, strings.NewReader(event.Body))
	if err != nil {
		return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusBadRequest}
	}
	res := &response{header: make(http.Header), status: http.StatusOK}
	discovery.Handler{Store: store, AuthorizeWrite: func(*http.Request) bool { return canWrite }}.ServeHTTP(res, req)
	headers := make(map[string]string, len(res.header))
	for name := range res.header {
		headers[name] = res.header.Get(name)
	}
	return events.APIGatewayV2HTTPResponse{StatusCode: res.status, Headers: headers, Body: res.body.String()}
}

type response struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (r *response) Header() http.Header         { return r.header }
func (r *response) Write(p []byte) (int, error) { return r.body.Write(p) }
func (r *response) WriteHeader(status int)      { r.status = status }
