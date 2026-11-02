package teardown

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
)

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func (e *Engine) load(fresh bool) (*Report, error) {
	if strings.TrimSpace(e.Scope.Application) == "" || strings.TrimSpace(e.Scope.Account) == "" {
		return nil, errors.New("teardown requires application and account scope")
	}
	if e.Store == nil || e.Driver == nil {
		return nil, errors.New("teardown requires driver and store")
	}
	r, err := e.Store.Load()
	if errors.Is(err, fs.ErrNotExist) && fresh {
		return &Report{Version: 1, Scope: e.Scope}, nil
	}
	if err != nil {
		return nil, errors.New("cannot load teardown report")
	}
	if r == nil || r.Version != 1 || r.Scope != e.Scope {
		return nil, errors.New("teardown report identity or version mismatch")
	}
	return r, nil
}
func (e *Engine) save(r *Report) error {
	r.UpdatedAt = now()
	if err := e.Store.Save(r); err != nil {
		return errors.New("cannot persist teardown report")
	}
	return nil
}
func mergeArtifacts(old, more []Artifact) []Artifact {
	m := map[string]int{}
	out := append([]Artifact(nil), old...)
	for i, a := range out {
		m[a.Key()] = i
	}
	for _, a := range more {
		if i, ok := m[a.Key()]; ok {
			out[i] = a
		} else {
			m[a.Key()] = len(out)
			out = append(out, a)
		}
	}
	return out
}
func complete(rs []Result) bool {
	for _, r := range rs {
		if r.Status != Absent && r.Status != Deleted && r.Status != Scheduled {
			return false
		}
	}
	return true
}
func (e *Engine) inspect(ctx context.Context, a Artifact) Result {
	r := Result{Artifact: a, CheckedAt: now()}
	if a.Kind == "inventory" {
		r.Status = Unverified
		r.Detail = "inventory unavailable"
		return r
	}
	if ctx.Err() != nil {
		r.Status = Unverified
		r.Detail = "operation canceled"
		return r
	}
	s, err := e.Driver.Inspect(ctx, a)
	if err != nil {
		r.Status = Unverified
		r.Detail = "verification failed"
		return r
	}
	switch s {
	case StateAbsent:
		r.Status = Absent
	case StatePresent:
		r.Status = Remains
	case StatePending:
		if a.Recovery && isKey(a) {
			r.Status = Scheduled
		} else {
			r.Status = Remains
		}
	default:
		r.Status = Unverified
		r.Detail = "unknown resource state"
	}
	return r
}
func (e *Engine) Nuke(ctx context.Context) (*Report, error) {
	if ctx == nil {
		return nil, errors.New("teardown requires context")
	}
	r, err := e.load(true)
	if err != nil {
		return nil, err
	}
	targets, err := e.Driver.Discover(ctx, e.Scope)
	var discoveryErr *DiscoveryError
	if err != nil && !errors.As(err, &discoveryErr) {
		return r, errors.New("target discovery failed")
	}
	cleanedDiscovery := r.Artifacts[:0]
	for _, a := range r.Artifacts {
		if a.Kind != "inventory" || a.Data["discovery"] != "true" {
			cleanedDiscovery = append(cleanedDiscovery, a)
		}
	}
	r.Artifacts = cleanedDiscovery
	if discoveryErr != nil {
		for _, issue := range discoveryErr.Artifacts {
			issue.Kind = "inventory"
			issue.Recovery = false
			data := map[string]string{}
			for k, v := range issue.Data {
				data[k] = v
			}
			data["discovery"] = "true"
			issue.Data = data
			r.Artifacts = mergeArtifacts(r.Artifacts, []Artifact{issue})
		}
		if len(discoveryErr.Artifacts) == 0 {
			r.Artifacts = mergeArtifacts(r.Artifacts, []Artifact{{ID: "discovery:" + e.Scope.Application, Kind: "inventory", Data: map[string]string{"discovery": "true"}}})
		}
	}
	tm := map[string]Target{}
	for _, t := range r.Targets {
		tm[t.ID] = t
	}
	for _, t := range targets {
		if old, ok := tm[t.ID]; ok && (old.Project != t.Project || old.Stack != t.Stack || old.Backend != t.Backend || old.Region != t.Region || old.Shared != t.Shared || old.Directory != t.Directory) {
			return r, errors.New("target identity mismatch")
		}
		tm[t.ID] = t
	}
	r.Targets = nil
	for _, t := range tm {
		r.Targets = append(r.Targets, t)
	}
	sort.Slice(r.Targets, func(i, j int) bool {
		a, b := r.Targets[i], r.Targets[j]
		if a.Shared != b.Shared {
			return !a.Shared
		}
		return a.ID < b.ID
	})
	authorizedRecovery := map[string]bool{}
	for _, a := range r.Artifacts {
		if a.Recovery {
			authorizedRecovery[a.Key()] = true
		}
	}
	if r.InventoryComplete == nil {
		r.InventoryComplete = map[string]bool{}
	}
	blocked := map[string]bool{}
	for _, t := range r.Targets {
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		as, ie := e.Driver.Inventory(ctx, t)
		if ie != nil && !errors.Is(ie, ErrAbsent) {
			blocked[t.ID] = true
		}
		if ie == nil {
			r.InventoryComplete[t.ID] = true
		}
		if ie == nil || (errors.Is(ie, ErrAbsent) && r.InventoryComplete[t.ID]) {
			cleaned := r.Artifacts[:0]
			for _, a := range r.Artifacts {
				if a.TargetID != t.ID || a.Kind != "inventory" || a.Data["discovery"] == "true" {
					cleaned = append(cleaned, a)
				}
			}
			r.Artifacts = cleaned
		}
		if ie != nil && (!errors.Is(ie, ErrAbsent) || !r.InventoryComplete[t.ID]) {
			as = append(as, Artifact{ID: t.ID, Kind: "inventory", TargetID: t.ID, Region: t.Region})
		}
		for i := range as {
			as[i].TargetID = t.ID
			as[i].Recovery = authorizedRecovery[as[i].Key()]
		}
		r.Artifacts = mergeArtifacts(r.Artifacts, as)
		r.Artifacts = mergeArtifacts(r.Artifacts, []Artifact{{ID: t.ID, Kind: "stack", TargetID: t.ID, Region: t.Region}})
	}
	recovery := []Artifact{}
	for _, a := range e.Recovery {
		a.Recovery = true
		recovery = append(recovery, a)
	}
	r.Artifacts = mergeArtifacts(r.Artifacts, recovery)
	hasCompleteInventory := false
	for _, target := range r.Targets {
		if r.InventoryComplete[target.ID] {
			hasCompleteInventory = true
			break
		}
	}
	scopeInventory := Artifact{ID: "scope:" + e.Scope.Application + ":" + e.Scope.Account, Kind: "inventory"}
	if hasCompleteInventory {
		cleaned := r.Artifacts[:0]
		for _, a := range r.Artifacts {
			if a.Key() != scopeInventory.Key() {
				cleaned = append(cleaned, a)
			}
		}
		r.Artifacts = cleaned
	} else {
		hasRecovery := false
		for _, a := range r.Artifacts {
			if a.Recovery {
				hasRecovery = true
				break
			}
		}
		if hasRecovery {
			r.Artifacts = mergeArtifacts(r.Artifacts, []Artifact{scopeInventory})
		}
	}
	r.Checks = nil
	r.CheckedAt = ""
	r.CheckScope = ""
	before := map[string]Status{}
	r.Results = nil
	for _, a := range r.Artifacts {
		if !a.Recovery {
			result := e.inspect(ctx, a)
			before[a.Key()] = result.Status
			r.Results = append(r.Results, result)
		}
	}
	if err = e.save(r); err != nil {
		return r, err
	}
	for _, t := range r.Targets {
		if ctx.Err() != nil {
			break
		}
		if !blocked[t.ID] {
			_ = e.Driver.Destroy(ctx, t)
		}
	}
	cleanupUnavailable := map[string]bool{}
	cleaner, canCleanup := e.Driver.(ArtifactCleaner)
	for _, a := range r.Artifacts {
		if a.Recovery || a.Data["cleanup"] != "true" {
			continue
		}
		if !canCleanup || blocked[a.TargetID] {
			cleanupUnavailable[a.Key()] = true
			continue
		}
		if ctx.Err() != nil {
			break
		}
		_ = cleaner.Cleanup(ctx, a)
	}
	r.Results = nil
	for _, a := range r.Artifacts {
		if !a.Recovery {
			result := e.inspect(ctx, a)
			if cleanupUnavailable[a.Key()] {
				result.Status = Unverified
				result.Detail = "artifact cleanup unavailable"
			}
			if result.Status == Absent && before[a.Key()] == Remains {
				result.Status = Deleted
			}
			r.Results = append(r.Results, result)
		}
	}
	if err = e.save(r); err != nil {
		return r, err
	}
	recovery = nil
	for _, a := range r.Artifacts {
		if a.Recovery {
			recovery = append(recovery, a)
		}
	}
	allowed := complete(r.Results) && ctx.Err() == nil
	preflight, ok := e.Driver.(RecoveryPreflight)
	if len(recovery) > 0 && allowed {
		if !ok || preflight.PrepareRecovery(ctx, recovery) != nil {
			allowed = false
		}
	}
	sort.SliceStable(recovery, func(i, j int) bool { return !isKey(recovery[i]) && isKey(recovery[j]) })
	bucketsOK := allowed
	for _, a := range recovery {
		res := Result{Artifact: a, Status: Retained, Detail: "recovery gate not satisfied", CheckedAt: now()}
		if allowed && (!isKey(a) || bucketsOK) && ctx.Err() == nil {
			if !isBucket(a) && !isKey(a) {
				res.Status = Unverified
				res.Detail = "unsupported recovery artifact"
				bucketsOK = false
			} else if er := e.Driver.RemoveRecovery(ctx, a); er != nil {
				res.Status = Failed
				res.Detail = "recovery removal failed"
				if !isKey(a) {
					bucketsOK = false
				}
			} else {
				res = e.inspect(ctx, a)
				if isKey(a) && res.Status == Scheduled {
					res.Detail = "key deletion scheduled"
				}
				if !isKey(a) && res.Status != Absent {
					bucketsOK = false
				}
			}
		}
		r.Results = append(r.Results, res)
		if err = e.save(r); err != nil {
			return r, err
		}
	}
	if ctx.Err() != nil {
		return r, ctx.Err()
	}
	if !complete(r.Results) {
		return r, ErrIncomplete
	}
	return r, nil
}
func isKey(a Artifact) bool {
	return a.Kind == "key" || a.Kind == "kms_key" || a.Kind == "recovery_key"
}
func isBucket(a Artifact) bool {
	return a.Kind == "bucket" || a.Kind == "s3_bucket" || a.Kind == "recovery_bucket"
}
func (e *Engine) Check(ctx context.Context, fast bool) (*Report, error) {
	if ctx == nil {
		return nil, errors.New("teardown requires context")
	}
	r, err := e.load(false)
	if err != nil {
		return nil, err
	}
	if fast && len(r.Checks) == 0 {
		return r, errors.New("fast check requires a previous full check")
	}
	previous := map[string]Result{}
	for _, v := range r.Checks {
		previous[v.Artifact.Key()] = v
	}
	checks := []Result{}
	for _, a := range r.Artifacts {
		old, exists := previous[a.Key()]
		if !fast || (exists && old.Status == Remains) {
			result := e.inspect(ctx, a)
			if result.Status == Scheduled {
				result.Status = Remains
				result.Detail = "key deletion pending"
			}
			checks = append(checks, result)
		} else if exists {
			checks = append(checks, old)
		} else {
			checks = append(checks, Result{Artifact: a, Status: Unverified, Detail: "not checked", CheckedAt: now()})
		}
	}
	r.Checks = checks
	r.CheckScope = "full"
	if fast {
		r.CheckScope = "fast"
	}
	r.CheckedAt = now()
	if err = e.save(r); err != nil {
		return r, err
	}
	if ctx.Err() != nil {
		return r, ctx.Err()
	}
	if !complete(checks) {
		return r, ErrIncomplete
	}
	return r, nil
}

// Summary is safe to print: it contains no provider error payloads.
func (r *Report) Summary() string {
	counts := map[Status]int{}
	for _, v := range r.Results {
		counts[v.Status]++
	}
	parts := []string{}
	for _, s := range []Status{Deleted, Absent, Remains, Unverified, Failed, Retained, Scheduled} {
		if counts[s] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", s, counts[s]))
		}
	}
	return strings.Join(parts, " ")
}
