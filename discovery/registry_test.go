package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type registryFixture struct {
	rev   int64
	entry *Entry
}

func (f *registryFixture) List(context.Context) ([]Entry, error) {
	if f.entry == nil {
		return nil, nil
	}
	return []Entry{*f.entry}, nil
}
func (f *registryFixture) Begin(context.Context, string) (int64, error) {
	f.rev++
	return f.rev, nil
}
func (f *registryFixture) Publish(_ context.Context, e Entry) error {
	if e.Revision != f.rev {
		return ErrSuperseded
	}
	f.entry = &e
	return nil
}
func (f *registryFixture) Retire(_ context.Context, _ string, rev int64) error {
	if rev != f.rev {
		return ErrSuperseded
	}
	f.entry = nil
	return nil
}

func TestFencedPublicationAndRetirement(t *testing.T) {
	h := Handler{Store: &registryFixture{}, AuthorizeWrite: func(r *http.Request) bool { return r.Header.Get("Fixture") == "writer" }}
	call := func(method, path, body string, writer bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if writer {
			req.Header.Set("Fixture", "writer")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	path := Path + "/dev/changes"
	if got := call(http.MethodPost, path, `{"operation":"begin"}`, false).Code; got != http.StatusForbidden {
		t.Fatalf("unauthorized write = %d", got)
	}

	begin := func() Receipt {
		t.Helper()
		w := call(http.MethodPost, path, `{"operation":"begin"}`, true)
		var r Receipt
		if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil || r.Revision < 1 {
			t.Fatalf("begin = %d %s", w.Code, w.Body.String())
		}
		return r
	}
	change := func(c Change, want int) {
		t.Helper()
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if w := call(http.MethodPost, path, string(b), true); w.Code != want {
			t.Fatalf("%s = %d %s", c.Operation, w.Code, w.Body.String())
		}
	}
	expect := func(n int) {
		t.Helper()
		w := call(http.MethodGet, Path, "", false)
		var idx Index
		if err := json.Unmarshal(w.Body.Bytes(), &idx); err != nil || idx.SchemaVersion != 1 || len(idx.Environments) != n {
			t.Fatalf("index = %d %s", w.Code, w.Body.String())
		}
	}
	first := begin()
	configuration := &Configuration{APIURL: "https://dev.components.example.test"}
	change(Change{Operation: "publish", Revision: first.Revision, Configuration: configuration}, http.StatusNoContent)
	second := begin()
	expect(1)
	change(Change{Operation: "publish", Revision: first.Revision, Configuration: configuration}, http.StatusConflict)
	change(Change{Operation: "retire", Revision: second.Revision}, http.StatusNoContent)
	expect(0)
}
