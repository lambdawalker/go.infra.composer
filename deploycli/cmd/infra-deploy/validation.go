package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

func verifySavedTarget(saved Settings, account, region, backend string) error {
	if saved.Account != "" && (account != "" && account != saved.Account || region != "" && region != saved.Region || backend != "" && backend != saved.Backend) {
		return errors.New("target differs from saved scope; use a separate project/environment or explicit migration")
	}
	return nil
}
func authenticatedIdentity(ci bool, profile string, read func() ([]byte, error), login func() error) ([]byte, error) {
	b, e := read()
	if e != nil && !ci {
		if e = login(); e != nil {
			return nil, e
		}
		return read()
	}
	return b, e
}

// Results retain input order, with bounded concurrency. A failed prerequisite
// prevents the caller from starting dependent setup/build/preview work.
func prerequisites(ctx context.Context, workers int, jobs []func() error) error {
	if workers < 1 || workers > 8 {
		return fmt.Errorf("workers must be 1..8")
	}
	var wg sync.WaitGroup
	queue := make(chan int)
	results := make([]error, len(jobs))
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range queue {
				if e := ctx.Err(); e != nil {
					results[index] = e
				} else {
					results[index] = jobs[index]()
				}
			}
		}()
	}
	for i := range jobs {
		queue <- i
	}
	close(queue)
	wg.Wait()
	for _, e := range results {
		if e != nil {
			return e
		}
	}
	return nil
}
func verifySSOProfile(ctx context.Context, profile string) error {
	for _, key := range []string{"sso_session", "sso_start_url"} {
		b, e := output(ctx, "aws", "configure", "get", key, "--profile", profile)
		if e == nil && strings.TrimSpace(string(b)) != "" {
			return nil
		}
	}
	return fmt.Errorf("profile %q is not configured for Identity Center; run aws configure sso --profile %s", profile, profile)
}

func validRegion(region string) bool { return pattern(`^[a-z]{2}(-[a-z]+)+-\d$`, region) }

func validBackend(backend string) bool { return pattern(`^s3://[a-z0-9.-]+/[^?\s]+$`, backend) }

func setRunEnv(key, value string) (func(), error) {
	old, present := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		return nil, err
	}
	return func() {
		if present {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	}, nil
}
