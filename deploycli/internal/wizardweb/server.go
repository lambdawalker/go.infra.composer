package wizardweb

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

//go:embed ui
var assets embed.FS

func Serve(ctx context.Context, title string, open bool, run func(context.Context, *Session) error) error {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	key := base64.RawURLEncoding.EncodeToString(token)
	host := listener.Addr().String()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	lifetime, closePage := context.WithCancel(ctx)
	defer closePage()
	session := newSession(title, cancel, closePage)
	session.Protect(os.Getenv("PULUMI_CONFIG_PASSPHRASE"), os.Getenv("AWS_SECRET_ACCESS_KEY"),
		os.Getenv("AWS_SESSION_TOKEN"), os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_TOKEN"),
		os.Getenv("CLOUDFLARE_API_TOKEN"), os.Getenv("PULUMI_ACCESS_TOKEN"))
	server := &http.Server{
		Handler: session.handler(host, key), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	url := "http://" + host + "/#" + key
	fmt.Fprintf(os.Stdout, "Deployment wizard: %s\nPress Ctrl+C to close the wizard.\n", url)
	if open {
		go openBrowser(lifetime, url)
	}
	finished := make(chan error, 1)
	go func() { finished <- runSession(runCtx, session, run) }()
	select {
	case <-lifetime.Done():
	case err = <-served:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}
	cancel()
	runErr := <-finished
	shutdownCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
		_ = server.Close()
	}
	if errors.Is(runErr, context.Canceled) {
		runErr = nil
	}
	return errors.Join(err, runErr)
}

func runSession(ctx context.Context, session *Session, run func(context.Context, *Session) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			var ok bool
			err, ok = recovered.(error)
			if !ok {
				err = fmt.Errorf("wizard stopped unexpectedly")
			}
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		session.finish(err)
	}()
	return run(ctx, session)
}

func openBrowser(ctx context.Context, url string) {
	openCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var args []string
	switch runtime.GOOS {
	case "windows":
		args = []string{"rundll32", "url.dll,FileProtocolHandler", url}
	case "darwin":
		args = []string{"open", url}
	default:
		args = []string{"xdg-open", url}
	}
	if err := exec.CommandContext(openCtx, args[0], args[1:]...).Run(); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "Open the deployment wizard URL above in your browser.")
	}
}

func (s *Session) handler(host, key string) http.Handler {
	static, err := fs.Sub(assets, "ui")
	if err != nil {
		panic(err)
	}
	files := http.FileServerFS(static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Host != host || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "Local wizard only", http.StatusForbidden)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && origin != "http://"+host {
			http.Error(w, "Invalid origin", http.StatusForbidden)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
				(r.URL.Path == "/" || r.URL.Path == "/wizard.css" || r.URL.Path == "/wizard.js") {
				files.ServeHTTP(w, r)
			} else {
				http.NotFound(w, r)
			}
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+key)) != 1 {
			http.Error(w, "Open the URL from your terminal", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/state":
			current := s.snapshot()
			after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
			if after == current.Revision {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(current)
		case r.Method == http.MethodPost && r.URL.Path == "/api/answer":
			var answer struct {
				ID    uint64
				Value string
			}
			r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
			d := json.NewDecoder(r.Body)
			d.DisallowUnknownFields()
			if d.Decode(&answer) != nil || d.Decode(new(any)) != io.EOF ||
				len(answer.Value) > 4096 || !validAnswer(answer.Value) {
				http.Error(w, "Invalid response", http.StatusBadRequest)
				return
			}
			if !s.answer(answer.ID, answer.Value) {
				http.Error(w, "This question has already changed", http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/stop":
			w.WriteHeader(http.StatusNoContent)
			s.stop()
		default:
			http.NotFound(w, r)
		}
	})
}

func validAnswer(value string) bool {
	for _, c := range value {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}
