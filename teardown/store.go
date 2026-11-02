package teardown

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

type FileStore struct{ Path string }

func (s FileStore) Load() (*Report, error) {
	f, e := os.Open(s.Path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	var r Report
	d := json.NewDecoder(io.LimitReader(f, 32<<20))
	if e = d.Decode(&r); e != nil {
		return nil, fmt.Errorf("invalid teardown report")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("invalid teardown report trailing data")
	}
	return &r, nil
}
func (s FileStore) Save(r *Report) error {
	dir := filepath.Dir(s.Path)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".teardown-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = json.NewEncoder(f).Encode(r); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(tmp, s.Path); e != nil {
		return e
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	d, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
