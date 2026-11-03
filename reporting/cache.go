package reporting

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// CachePath returns a filename scoped by every Scope field.
func CachePath(dir string, scope Scope) string {
	b, _ := json.Marshal(scope)
	h := sha256.Sum256(b)
	return filepath.Join(dir, hex.EncodeToString(h[:])+".json")
}
func LoadCache(path string, scope Scope) (Report, error) {
	var r Report
	b, e := os.ReadFile(path)
	if e != nil {
		return r, e
	}
	if e = json.Unmarshal(b, &r); e != nil {
		return Report{}, e
	}
	if r.SchemaVersion != SchemaVersion {
		return Report{}, errors.New("unsupported report schema")
	}
	if r.Scope != scope {
		return Report{}, errors.New("report scope mismatch")
	}
	r.CacheLoaded = true
	return Normalize(r), nil
}

// SaveCache atomically replaces a private snapshot. Parent directories are created private.
func SaveCache(path string, r Report) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".report-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		e = WriteJSON(f, r)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(f.Name(), path)
}
