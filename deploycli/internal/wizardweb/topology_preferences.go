package wizardweb

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type topologyPreference struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (p topologyPreference) valid() bool {
	switch p.Key {
	case "arrows", "focus":
		return p.Value == "true" || p.Value == "false"
	case "platform", "glass":
		n, err := strconv.ParseFloat(p.Value, 64)
		return err == nil && n >= 0 && n <= 100
	}
	return strings.HasPrefix(p.Key, "icon:") && len(p.Key) > 5 && len(p.Key) <= 4096 && (p.Value == "/models/aws/acm.glb" || p.Value == "/models/aws/acm-shield.glb")
}

// Separate files let concurrent wizard instances update different choices independently.
func (s *Session) topologyPreferences(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if len(project) > 2048 {
		http.Error(w, "Invalid project", 400)
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, "Preferences unavailable", 500)
		return
	}
	dir := filepath.Join(home, ".infra-composer", "_view-preferences", fmt.Sprintf("%x", sha256.Sum256([]byte(s.repository+"\x00"+project))))
	if r.Method == http.MethodGet {
		entries, err := os.ReadDir(dir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			http.Error(w, "Preferences unavailable", 500)
			return
		}
		values := map[string]string{}
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			var p topologyPreference
			if err == nil && json.Unmarshal(b, &p) == nil && p.valid() {
				values[p.Key] = p.Value
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(values)
		return
	}
	var p topologyPreference
	if !decodeReply(w, r, &p) || !p.valid() {
		http.Error(w, "Invalid topology preference", 400)
		return
	}
	if err = os.MkdirAll(dir, 0700); err == nil {
		var f *os.File
		if f, err = os.CreateTemp(dir, ".pending-"); err == nil {
			defer os.Remove(f.Name())
			err = json.NewEncoder(f).Encode(p)
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(f.Name(), filepath.Join(dir, fmt.Sprintf("%x.json", sha256.Sum256([]byte(p.Key)))))
			}
		}
	}
	if err != nil {
		http.Error(w, "Could not save topology preference", 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
