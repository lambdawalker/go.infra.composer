// Package discovery defines the public environment index and its update protocol.
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

const Path = "/v1/environments"

var (
	ErrSuperseded = errors.New("discovery update was superseded")
	environmentID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	publicID      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

type Configuration struct {
	APIURL            string `json:"apiUrl"`
	CognitoIssuer     string `json:"cognitoIssuer,omitempty"`
	CognitoUserPoolID string `json:"cognitoUserPoolId,omitempty"`
	CognitoClientID   string `json:"cognitoClientId,omitempty"`
}

type Entry struct {
	ID            string        `json:"id" dynamodbav:"id"`
	Revision      int64         `json:"revision" dynamodbav:"revision"`
	Configuration Configuration `json:"configuration" dynamodbav:"configuration"`
}

type Index struct {
	SchemaVersion int     `json:"schemaVersion"`
	Environments  []Entry `json:"environments"`
}

type Change struct {
	Operation     string         `json:"operation"`
	Revision      int64          `json:"revision,omitempty"`
	Configuration *Configuration `json:"configuration,omitempty"`
}

type Receipt struct {
	Revision int64 `json:"revision"`
}

// Store must preserve the last published entry when reserving a revision.
// Updates with a superseded revision must fail atomically.
type Store interface {
	List(context.Context) ([]Entry, error)
	Begin(context.Context, string) (int64, error)
	Publish(context.Context, Entry) error
	Retire(context.Context, string, int64) error
}

func ValidEnvironment(id string) bool {
	return len(id) <= 32 && environmentID.MatchString(id)
}

func (c Configuration) Validate() error {
	if !safeHTTPS(c.APIURL, false) {
		return errors.New("invalid HTTPS API origin")
	}
	if c.CognitoIssuer == "" && c.CognitoUserPoolID == "" && c.CognitoClientID == "" {
		return nil
	}
	if !safeHTTPS(c.CognitoIssuer, true) || !publicID.MatchString(c.CognitoUserPoolID) ||
		!publicID.MatchString(c.CognitoClientID) {
		return errors.New("invalid Cognito discovery configuration")
	}
	return nil
}

func safeHTTPS(value string, allowPath bool) bool {
	if len(value) > 1024 || strings.ContainsAny(value, "#?") {
		return false
	}
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil &&
		(allowPath || u.EscapedPath() == "" || u.EscapedPath() == "/")
}

// Handler serves the discovery protocol. The hosting adapter supplies an
// authorization check for write requests.
type Handler struct {
	Store          Store
	AuthorizeWrite func(*http.Request) bool
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet && r.URL.Path == Path {
		entries, err := h.Store.List(r.Context())
		if err != nil {
			http.Error(w, "discovery unavailable", http.StatusServiceUnavailable)
			return
		}
		if entries == nil {
			entries = []Entry{}
		}
		slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(a.ID, b.ID) })
		writeJSON(w, Index{SchemaVersion: 1, Environments: entries})
		return
	}

	id, found := strings.CutPrefix(r.URL.Path, Path+"/")
	id, suffix := strings.CutSuffix(id, "/changes")
	if r.Method != http.MethodPost || !found || !suffix || !ValidEnvironment(id) {
		http.NotFound(w, r)
		return
	}
	if h.AuthorizeWrite == nil || !h.AuthorizeWrite(r) {
		http.Error(w, "publication requires authorization", http.StatusForbidden)
		return
	}

	var change Change
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(&change) != nil || d.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid discovery change", http.StatusBadRequest)
		return
	}

	var err error
	switch {
	case change.Operation == "begin" && change.Revision == 0 && change.Configuration == nil:
		var rev int64
		rev, err = h.Store.Begin(r.Context(), id)
		if err == nil {
			writeJSON(w, Receipt{Revision: rev})
			return
		}
	case change.Operation == "publish" && change.Revision > 0 && change.Configuration != nil && change.Configuration.Validate() == nil:
		err = h.Store.Publish(r.Context(), Entry{ID: id, Revision: change.Revision, Configuration: *change.Configuration})
	case change.Operation == "retire" && change.Revision > 0 && change.Configuration == nil:
		err = h.Store.Retire(r.Context(), id, change.Revision)
	default:
		http.Error(w, "invalid discovery change", http.StatusBadRequest)
		return
	}
	if errors.Is(err, ErrSuperseded) {
		http.Error(w, "discovery revision superseded", http.StatusConflict)
	} else if err != nil {
		http.Error(w, "discovery unavailable", http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
