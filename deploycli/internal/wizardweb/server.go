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
	"strconv"
	"strings"
	"time"
)

//go:embed ui
var assets embed.FS

var awsIconPaths = map[string]struct{}{
	"/icons/aws/acm.svg": {}, "/icons/aws/api-gateway.svg": {},
	"/icons/aws/cloudwatch.svg": {}, "/icons/aws/cognito.svg": {},
	"/icons/aws/dynamodb.svg": {}, "/icons/aws/eventbridge.svg": {},
	"/icons/aws/iam.svg": {}, "/icons/aws/kms.svg": {},
	"/icons/aws/lambda.svg": {}, "/icons/aws/route53.svg": {},
	"/icons/aws/s3.svg": {}, "/icons/aws/secretsmanager.svg": {},
	"/icons/aws/ses.svg": {},
}

var awsModelPaths = func() map[string]struct{} {
	paths := make(map[string]struct{}, len(awsIconPaths))
	for icon := range awsIconPaths {
		paths[strings.Replace(strings.TrimSuffix(icon, ".svg"), "/icons/", "/models/", 1)+".glb"] = struct{}{}
	}
	return paths
}()

func Serve(ctx context.Context, title string, open bool, register func(int, string) (func() error, error), run func(context.Context, *Session, bool) error) (result error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	instanceID := rand.Text()
	unregister, err := register(listener.Addr().(*net.TCPAddr).Port, instanceID)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, unregister()) }()

	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	key := base64.RawURLEncoding.EncodeToString(token)
	host := listener.Addr().String()
	lifetime, closePage := context.WithCancel(ctx)
	defer closePage()
	session := newSession(title, closePage)
	session.instanceID = instanceID
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
	go func() { finished <- runSession(lifetime, session, run) }()
	select {
	case <-lifetime.Done():
	case err = <-served:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}
	closePage()
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

func runSession(ctx context.Context, session *Session, run func(context.Context, *Session, bool) error) error {
	defer session.ForgetVault()
	for showActions := false; ; showActions = true {
		session.Protect(os.Getenv("PULUMI_CONFIG_PASSPHRASE"), os.Getenv("AWS_SECRET_ACCESS_KEY"),
			os.Getenv("AWS_SESSION_TOKEN"), os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_TOKEN"),
			os.Getenv("CLOUDFLARE_API_TOKEN"), os.Getenv("PULUMI_ACCESS_TOKEN"))
		operation, cancel := session.startOperation(ctx)
		err := runOperation(operation, session, showActions, run)
		cancel()
		home := session.finish(err)
		select {
		case <-ctx.Done():
			return err
		case <-home:
		}
	}
}

func runOperation(ctx context.Context, session *Session, showActions bool, run func(context.Context, *Session, bool) error) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
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
	}()
	return run(ctx, session, showActions)
}

func (s *Session) handler(host, key string) http.Handler {
	static, err := fs.Sub(assets, "ui")
	if err != nil {
		panic(err)
	}
	files := http.FileServerFS(static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
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
		if r.URL.Path == "/" && s.instanceID != "" {
			// A local index reader can distinguish a stale port from this instance.
			w.Header().Set("X-Infra-Composer-Instance", s.instanceID)
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			_, isAWSIcon := awsIconPaths[r.URL.Path]
			_, isAWSModel := awsModelPaths[r.URL.Path]
			if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
				(r.URL.Path == "/" || r.URL.Path == "/wizard.css" || r.URL.Path == "/wizard.js" ||
					r.URL.Path == "/workspace.css" || r.URL.Path == "/workspace.js" ||
					r.URL.Path == "/topology.css" || r.URL.Path == "/topology.js" ||
					r.URL.Path == "/aws-icons.js" || r.URL.Path == "/environment-menu.js" || r.URL.Path == "/vendor/topology-three.js" ||
					isAWSIcon || isAWSModel) {
				if isAWSModel {
					w.Header().Set("Content-Type", "model/gltf-binary")
				}
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
		case r.Method == http.MethodPost && r.URL.Path == "/api/activity":
			s.recordActivity()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/unlock":
			var unlock struct {
				Passphrase string `json:"passphrase"`
			}
			if !decodeReply(w, r, &unlock) || !validAnswer(unlock.Passphrase) {
				http.Error(w, "Invalid passphrase", http.StatusBadRequest)
				return
			}
			if !s.unlockVault(unlock.Passphrase) {
				http.Error(w, "The vault passphrase didn’t match.", http.StatusUnprocessableEntity)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/form-preview":
			var draft struct {
				ID     uint64            `json:"id"`
				Values map[string]string `json:"values"`
			}
			if !decodeReply(w, r, &draft) || !validReply(reply{Values: draft.Values}) {
				http.Error(w, "Invalid draft", http.StatusBadRequest)
				return
			}
			values, ok := s.preview(draft.ID, draft.Values)
			if !ok {
				http.Error(w, "This question has already changed", http.StatusConflict)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(values)
		case r.Method == http.MethodPost && r.URL.Path == "/api/environment":
			var selection struct {
				ID   uint64 `json:"id"`
				Mode string `json:"mode"`
				Name string `json:"name"`
			}
			if !decodeReply(w, r, &selection) {
				http.Error(w, "Invalid environment selection", http.StatusBadRequest)
				return
			}
			if err := s.selectEnvironment(selection.ID, selection.Mode, selection.Name); err != nil {
				status := http.StatusUnprocessableEntity
				if errors.Is(err, errEnvironmentUnavailable) {
					status = http.StatusConflict
				}
				http.Error(w, err.Error(), status)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/answer":
			var answer struct {
				ID uint64 `json:"id"`
				reply
			}
			if !decodeReply(w, r, &answer) || !validReply(answer.reply) {
				http.Error(w, "Invalid response", http.StatusBadRequest)
				return
			}
			if !s.answer(answer.ID, answer.reply) {
				http.Error(w, "This question has already changed", http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/actions":
			var navigation struct {
				Revision uint64 `json:"revision"`
			}
			if !decodeReply(w, r, &navigation) {
				http.Error(w, "Invalid request", http.StatusBadRequest)
				return
			}
			if !s.returnToActions(navigation.Revision) {
				http.Error(w, "The action menu is unavailable", http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/stop":
			var control struct {
				Run   uint64 `json:"run"`
				Close bool   `json:"close"`
			}
			if !decodeReply(w, r, &control) {
				http.Error(w, "Invalid request", http.StatusBadRequest)
				return
			}
			if !s.stop(control.Run, control.Close) {
				http.Error(w, "The operation has changed", http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
}

func decodeReply(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(value) == nil && d.Decode(new(any)) == io.EOF
}

func validReply(answer reply) bool {
	if answer.Value != nil {
		return answer.Values == nil && validAnswer(*answer.Value)
	}
	if answer.Values == nil {
		return false
	}
	for _, value := range answer.Values {
		if !validAnswer(value) {
			return false
		}
	}
	return true
}

func validAnswer(value string) bool {
	if len(value) > 4096 {
		return false
	}
	for _, c := range value {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}
