package teardown

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

type memoryStore struct {
	r     *Report
	fail  bool
	saves int
}

func (s *memoryStore) Load() (*Report, error) {
	if s.r == nil {
		return nil, fs.ErrNotExist
	}
	return s.r, nil
}
func (s *memoryStore) Save(r *Report) error {
	s.saves++
	if s.fail {
		return errors.New("secret")
	}
	c := *r
	c.Artifacts = append([]Artifact(nil), r.Artifacts...)
	s.r = &c
	return nil
}

type fakeDriver struct {
	targets      []Target
	artifacts    map[string][]Artifact
	states       map[string]State
	inventoryErr map[string]error
	destroyErr   map[string]error
	inspectErr   map[string]error
	operations   []string
	preflightErr error
	store        *memoryStore
	afterDestroy func()
	discoverErr  error
}

func (d *fakeDriver) Discover(context.Context, Scope) ([]Target, error) {
	d.operations = append(d.operations, "discover")
	return d.targets, d.discoverErr
}
func (d *fakeDriver) Inventory(_ context.Context, t Target) ([]Artifact, error) {
	return d.artifacts[t.ID], d.inventoryErr[t.ID]
}
func (d *fakeDriver) Destroy(_ context.Context, t Target) error {
	if d.store != nil && d.store.saves == 0 {
		panic("destroy before save")
	}
	d.operations = append(d.operations, "destroy:"+t.ID)
	if d.afterDestroy != nil {
		d.afterDestroy()
	}
	return d.destroyErr[t.ID]
}
func (d *fakeDriver) Inspect(_ context.Context, a Artifact) (State, error) {
	d.operations = append(d.operations, "inspect:"+a.ID)
	s := d.states[a.ID]
	if s == "" {
		s = StateAbsent
	}
	return s, d.inspectErr[a.ID]
}
func (d *fakeDriver) RemoveRecovery(_ context.Context, a Artifact) error {
	d.operations = append(d.operations, "remove:"+a.ID)
	return nil
}
func (d *fakeDriver) PrepareRecovery(context.Context, []Artifact) error {
	d.operations = append(d.operations, "prepare")
	return d.preflightErr
}
func TestNukePersistsBeforeDestroyAndContinuesIndependentFailures(t *testing.T) {
	s := &memoryStore{}
	d := &fakeDriver{store: s, targets: []Target{{ID: "shared", Shared: true}, {ID: "b"}, {ID: "a"}}, artifacts: map[string][]Artifact{"a": {{ID: "resource", Kind: "bucket", TargetID: "a"}}}, destroyErr: map[string]error{"a": errors.New("provider token")}, states: map[string]State{"resource": StatePresent}}
	r, err := (&Engine{Scope: Scope{Application: "app", Account: "123"}, Store: s, Driver: d}).Nuke(context.Background())
	if err == nil {
		t.Fatal("expected incomplete")
	}
	var destroyed []string
	for _, op := range d.operations {
		if len(op) > 8 && op[:8] == "destroy:" {
			destroyed = append(destroyed, op)
		}
	}
	if !reflect.DeepEqual(destroyed, []string{"destroy:a", "destroy:b", "destroy:shared"}) {
		t.Fatal(destroyed)
	}
	if len(r.Artifacts) != 4 {
		t.Fatal(r.Artifacts)
	}
	if r.Results[0].Detail == "provider token" {
		t.Fatal("leaked error")
	}
}
func TestCheckFastUsesLatestRemainsAndPreservesUnknown(t *testing.T) {
	a := Artifact{ID: "a", Kind: "bucket"}
	b := Artifact{ID: "b", Kind: "bucket"}
	s := &memoryStore{r: &Report{Version: 1, Scope: Scope{Application: "app", Account: "123"}, Artifacts: []Artifact{a, b}, Checks: []Result{{Artifact: a, Status: Remains}, {Artifact: b, Status: Unverified}}}}
	d := &fakeDriver{}
	r, err := (&Engine{Scope: s.r.Scope, Driver: d, Store: s}).Check(context.Background(), true)
	if err == nil {
		t.Fatal("unknown must remain unsuccessful")
	}
	if !reflect.DeepEqual(d.operations, []string{"inspect:a"}) {
		t.Fatal(d.operations)
	}
	if r.Checks[1].Status != Unverified {
		t.Fatal(r.Checks)
	}
}
func TestRecoveryBucketsMustBeAbsentBeforeKeys(t *testing.T) {
	s := &memoryStore{}
	d := &fakeDriver{targets: []Target{{ID: "app"}}, states: map[string]State{"bucket": StatePresent}}
	e := Engine{Scope: Scope{Application: "app", Account: "123"}, Store: s, Driver: d, Recovery: []Artifact{{ID: "key", Kind: "recovery_key", Recovery: true}, {ID: "bucket", Kind: "recovery_bucket", Recovery: true}}}
	_, err := e.Nuke(context.Background())
	if err == nil {
		t.Fatal("expected remains")
	}
	for _, op := range d.operations {
		if op == "remove:key" {
			t.Fatal("scheduled key while bucket exists")
		}
	}
}
func TestFileStoreRoundTripAndScopeMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	s := FileStore{Path: path}
	if _, err := s.Load(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	r := &Report{Version: 1, Scope: Scope{Application: "app", Account: "123"}}
	if err := s.Save(r); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil || got.Scope != r.Scope {
		t.Fatal(got, err)
	}
	d := &fakeDriver{}
	_, err = (&Engine{Scope: Scope{Application: "other", Account: "123"}, Driver: d, Store: s}).Nuke(context.Background())
	if err == nil || len(d.operations) > 0 {
		t.Fatal("scope mismatch contacted driver")
	}
}
func TestPendingPrimaryResourceBlocksRecovery(t *testing.T) {
	s := &memoryStore{}
	d := &fakeDriver{targets: []Target{{ID: "app"}}, states: map[string]State{"app": StatePending}}
	r, err := (&Engine{Scope: Scope{Application: "app", Account: "123"}, Driver: d, Store: s, Recovery: []Artifact{{ID: "key", Kind: "recovery_key"}}}).Nuke(context.Background())
	if err == nil {
		t.Fatal("pending primary resource must block recovery")
	}
	for _, op := range d.operations {
		if op == "remove:key" {
			t.Fatal("recovery removed before primary absent")
		}
	}
	if r.Results[0].Status != Remains {
		t.Fatal(r.Results)
	}
}
func TestSaveFailurePreventsDestruction(t *testing.T) {
	s := &memoryStore{fail: true}
	d := &fakeDriver{targets: []Target{{ID: "app"}}}
	_, err := (&Engine{Scope: Scope{Application: "app", Account: "123"}, Driver: d, Store: s}).Nuke(context.Background())
	if err == nil {
		t.Fatal("expected save failure")
	}
	for _, op := range d.operations {
		if op == "destroy:app" {
			t.Fatal("destroy after failed persistence")
		}
	}
}
func TestPriorInventorySurvivesMissingStackAndFullCheckUsesOnlySavedIDs(t *testing.T) {
	a := Artifact{ID: "orphan", Kind: "bucket", TargetID: "app"}
	s := &memoryStore{r: &Report{Version: 1, Scope: Scope{Application: "app", Account: "123"}, Targets: []Target{{ID: "app"}}, InventoryComplete: map[string]bool{"app": true}, Artifacts: []Artifact{a}}}
	d := &fakeDriver{inventoryErr: map[string]error{"app": ErrAbsent}, states: map[string]State{"orphan": StatePresent}}
	e := &Engine{Scope: s.r.Scope, Driver: d, Store: s}
	r, err := e.Nuke(context.Background())
	if err == nil || len(r.Artifacts) != 2 {
		t.Fatal(r, err)
	}
	d.operations = nil
	_, err = e.Check(context.Background(), false)
	if err == nil {
		t.Fatal("expected remains")
	}
	if !reflect.DeepEqual(d.operations, []string{"inspect:orphan", "inspect:app"}) {
		t.Fatal(d.operations)
	}
}
func TestRecoveryPreflightFailureRetainsArtifacts(t *testing.T) {
	s := &memoryStore{}
	d := &fakeDriver{targets: []Target{{ID: "app"}}, preflightErr: errors.New("secret")}
	r, err := (&Engine{Scope: Scope{Application: "app", Account: "123"}, Driver: d, Store: s, Recovery: []Artifact{{ID: "bucket", Kind: "recovery_bucket"}}}).Nuke(context.Background())
	if err == nil || r.Results[len(r.Results)-1].Status != Retained {
		t.Fatal(r, err)
	}
	for _, op := range d.operations {
		if op == "remove:bucket" {
			t.Fatal("removed after failed preflight")
		}
	}
}
func TestNewInventoryCannotAuthorizeRecoveryDeletion(t *testing.T) {
	s := &memoryStore{}
	d := &fakeDriver{targets: []Target{{ID: "app"}}, artifacts: map[string][]Artifact{"app": {{ID: "unsolicited", Kind: "recovery_bucket", Recovery: true}}}}
	r, err := (&Engine{Scope: Scope{Application: "app", Account: "123"}, Driver: d, Store: s}).Nuke(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range d.operations {
		if op == "remove:unsolicited" {
			t.Fatal("inventory authorized recovery deletion")
		}
	}
	for _, a := range r.Artifacts {
		if a.ID == "unsolicited" && a.Recovery {
			t.Fatal("new inventory treated as explicit recovery")
		}
	}
}
func TestNukeDistinguishesDeletedAndClearsChecks(t *testing.T) {
	s := &memoryStore{r: &Report{Version: 1, Scope: Scope{Application: "app", Account: "123"}, Checks: []Result{{Status: Remains}}, CheckedAt: "old", CheckScope: "full"}}
	d := &fakeDriver{targets: []Target{{ID: "app"}}, states: map[string]State{"app": StatePresent}}
	d.afterDestroy = func() { d.states["app"] = StateAbsent }
	r, err := (&Engine{Scope: s.r.Scope, Driver: d, Store: s}).Nuke(context.Background())
	if err != nil || r.Results[0].Status != Deleted {
		t.Fatal(r, err)
	}
	if len(r.Checks) != 0 || r.CheckedAt != "" || r.CheckScope != "" {
		t.Fatal("stale checks")
	}
}
func TestInventoryUncertaintyTracksLatestAttempt(t *testing.T) {
	scope := Scope{Application: "app", Account: "123"}
	s := &memoryStore{r: &Report{Version: 1, Scope: scope, Targets: []Target{{ID: "app"}}, Artifacts: []Artifact{{ID: "resource", Kind: "bucket", TargetID: "app"}}}}
	d := &fakeDriver{inventoryErr: map[string]error{"app": errors.New("denied")}}
	e := &Engine{Scope: scope, Store: s, Driver: d}
	r, err := e.Nuke(context.Background())
	if err == nil {
		t.Fatal("denied inventory certified empty")
	}
	d.inventoryErr = nil
	r, err = e.Nuke(context.Background())
	if err != nil {
		t.Fatal(r, err)
	}
	for _, a := range r.Artifacts {
		if a.Kind == "inventory" {
			t.Fatal("stale uncertainty")
		}
	}
}
func TestCheckPendingKeyRemains(t *testing.T) {
	scope := Scope{Application: "app", Account: "123"}
	s := &memoryStore{r: &Report{Version: 1, Scope: scope, Artifacts: []Artifact{{ID: "key", Kind: "recovery_key", Recovery: true}}}}
	d := &fakeDriver{states: map[string]State{"key": StatePending}}
	r, err := (&Engine{Scope: scope, Store: s, Driver: d}).Check(context.Background(), false)
	if err == nil || r.Checks[0].Status != Remains {
		t.Fatal(r, err)
	}
}
func TestEmptyScopeAndNilContextRejected(t *testing.T) {
	s := &memoryStore{}
	d := &fakeDriver{}
	e := &Engine{Store: s, Driver: d}
	if _, err := e.Nuke(context.Background()); err == nil {
		t.Fatal("empty scope accepted")
	}
	e.Scope = Scope{Application: "app", Account: "123"}
	if _, err := e.Nuke(nil); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := e.Check(nil, false); err == nil {
		t.Fatal("nil context accepted")
	}
}
func TestRecoveryDeletesEveryBucketBeforeSchedulingKey(t *testing.T) {
	scope := Scope{Application: "app", Account: "123"}
	s := &memoryStore{}
	d := &fakeDriver{targets: []Target{{ID: "app"}}, states: map[string]State{"key": StatePending}}
	e := &Engine{Scope: scope, Driver: d, Store: s, Recovery: []Artifact{{ID: "key", Kind: "recovery_key"}, {ID: "bucket-b", Kind: "recovery_bucket"}, {ID: "bucket-a", Kind: "recovery_bucket"}}}
	r, err := e.Nuke(context.Background())
	if err != nil {
		t.Fatal(r, err)
	}
	want := []string{"discover", "inspect:app", "destroy:app", "inspect:app", "prepare", "remove:bucket-b", "inspect:bucket-b", "remove:bucket-a", "inspect:bucket-a", "remove:key", "inspect:key"}
	if !reflect.DeepEqual(d.operations, want) {
		t.Fatal(d.operations)
	}
	if r.Results[len(r.Results)-1].Status != Scheduled {
		t.Fatal(r.Results)
	}
}
func TestFileStorePrivatePermissionsAndMalformedData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	s := FileStore{Path: path}
	if err := s.Save(&Report{Version: 1}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
	if err = os.WriteFile(path, []byte("{\"version\":1} trailing"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Load(); err == nil {
		t.Fatal("accepted malformed report")
	}
}

type cleanupDriver struct {
	*fakeDriver
	cleanupErrors map[string]error
}

func (d *cleanupDriver) Cleanup(_ context.Context, a Artifact) error {
	if d.store != nil && d.store.saves == 0 {
		panic("cleanup before persistence")
	}
	d.operations = append(d.operations, "cleanup:"+a.ID)
	if err := d.cleanupErrors[a.ID]; err != nil {
		return err
	}
	if d.states == nil {
		d.states = map[string]State{}
	}
	d.states[a.ID] = StateAbsent
	return nil
}
func TestCleanupRunsAfterDestroyAndVerifiesSavedArtifactsOnRetry(t *testing.T) {
	scope := Scope{Application: "app", Account: "123"}
	a := Artifact{ID: "orphan", Kind: "logs", TargetID: "app", Data: map[string]string{"cleanup": "true"}}
	s := &memoryStore{r: &Report{Version: 1, Scope: scope, Targets: []Target{{ID: "app"}}, InventoryComplete: map[string]bool{"app": true}, Artifacts: []Artifact{a}}}
	d := &cleanupDriver{fakeDriver: &fakeDriver{store: s, inventoryErr: map[string]error{"app": ErrAbsent}, states: map[string]State{"orphan": StatePresent}}}
	r, err := (&Engine{Scope: scope, Driver: d, Store: s}).Nuke(context.Background())
	if err != nil {
		t.Fatal(r, err)
	}
	destroy, cleanup := -1, -1
	for i, op := range d.operations {
		if op == "destroy:app" {
			destroy = i
		}
		if op == "cleanup:orphan" {
			cleanup = i
		}
	}
	if destroy < 0 || cleanup <= destroy {
		t.Fatal(d.operations)
	}
	if r.Results[0].Status != Deleted {
		t.Fatal(r.Results)
	}
}
func TestCleanupUnsupportedCannotReleaseRecoveryGate(t *testing.T) {
	scope := Scope{Application: "app", Account: "123"}
	s := &memoryStore{r: &Report{Version: 1, Scope: scope, Artifacts: []Artifact{{ID: "orphan", Kind: "logs", Data: map[string]string{"cleanup": "true"}}}}}
	d := &fakeDriver{}
	r, err := (&Engine{Scope: scope, Driver: d, Store: s, Recovery: []Artifact{{ID: "key", Kind: "recovery_key"}}}).Nuke(context.Background())
	if err == nil || r.Results[0].Status != Unverified {
		t.Fatal(r, err)
	}
	for _, op := range d.operations {
		if op == "remove:key" {
			t.Fatal("removed recovery despite unavailable cleanup")
		}
	}
}
func TestCleanupContinuesIndependentFailures(t *testing.T) {
	scope := Scope{Application: "app", Account: "123"}
	s := &memoryStore{r: &Report{Version: 1, Scope: scope, Artifacts: []Artifact{{ID: "a", Kind: "logs", Data: map[string]string{"cleanup": "true"}}, {ID: "b", Kind: "logs", Data: map[string]string{"cleanup": "true"}}}}}
	d := &cleanupDriver{fakeDriver: &fakeDriver{states: map[string]State{"a": StatePresent, "b": StatePresent}}, cleanupErrors: map[string]error{"a": errors.New("secret")}}
	r, err := (&Engine{Scope: scope, Driver: d, Store: s}).Nuke(context.Background())
	if err == nil || r.Results[0].Status != Remains || r.Results[1].Status != Deleted {
		t.Fatal(r, err)
	}
	if r.Results[0].Detail == "secret" {
		t.Fatal("provider body persisted")
	}
}
func TestInventoryOwnershipFailurePreventsTargetDestroy(t *testing.T) {
	scope := Scope{Application: "app", Account: "123"}
	s := &memoryStore{}
	d := &fakeDriver{targets: []Target{{ID: "app"}}, inventoryErr: map[string]error{"app": errors.New("ownership denied")}}
	r, err := (&Engine{Scope: scope, Driver: d, Store: s}).Nuke(context.Background())
	if err == nil {
		t.Fatal(r)
	}
	for _, op := range d.operations {
		if op == "destroy:app" {
			t.Fatal("destroyed ownership-blocked target")
		}
	}
}
func TestInventoryCompleteSurvivesDenialThenAbsent(t *testing.T) {
	scope := Scope{Application: "app", Account: "123"}
	s := &memoryStore{}
	d := &fakeDriver{targets: []Target{{ID: "app"}}, artifacts: map[string][]Artifact{"app": {{ID: "resource", Kind: "bucket"}}}}
	e := &Engine{Scope: scope, Driver: d, Store: s}
	if _, err := e.Nuke(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.inventoryErr = map[string]error{"app": errors.New("denied")}
	if _, err := e.Nuke(context.Background()); err == nil {
		t.Fatal("denial not reported")
	}
	d.inventoryErr["app"] = ErrAbsent
	d.artifacts = nil
	r, err := e.Nuke(context.Background())
	if err != nil {
		t.Fatal(r, err)
	}
	for _, a := range r.Artifacts {
		if a.Kind == "inventory" {
			t.Fatal("stale sentinel")
		}
	}
}
func TestPartialInventoryDoesNotProveCompletenessOnAbsent(t *testing.T) {
	scope := Scope{Application: "app", Account: "123"}
	s := &memoryStore{}
	d := &fakeDriver{targets: []Target{{ID: "app"}}, artifacts: map[string][]Artifact{"app": {{ID: "partial", Kind: "bucket"}}}, inventoryErr: map[string]error{"app": errors.New("partial failure")}}
	e := &Engine{Scope: scope, Driver: d, Store: s}
	if _, err := e.Nuke(context.Background()); err == nil {
		t.Fatal("partial failure not reported")
	}
	d.inventoryErr["app"] = ErrAbsent
	d.artifacts = nil
	if _, err := e.Nuke(context.Background()); err == nil {
		t.Fatal("partial inventory certified absence")
	}
}
func TestCompleteEmptyInventoryAllowsAbsentRetry(t *testing.T) {
	scope := Scope{Application: "app", Account: "123"}
	s := &memoryStore{}
	d := &fakeDriver{targets: []Target{{ID: "app"}}}
	e := &Engine{Scope: scope, Driver: d, Store: s}
	if _, err := e.Nuke(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.inventoryErr = map[string]error{"app": ErrAbsent}
	if r, err := e.Nuke(context.Background()); err != nil {
		t.Fatal(r, err)
	}
}
func TestFreshEmptyDiscoveryCannotDeleteRecovery(t *testing.T) {
	scope := Scope{Application: "app", Account: "123"}
	s := &memoryStore{}
	d := &fakeDriver{}
	e := &Engine{Scope: scope, Driver: d, Store: s, Recovery: []Artifact{{ID: "key", Kind: "recovery_key"}}}
	r, err := e.Nuke(context.Background())
	if err == nil {
		t.Fatal("empty discovery authorized recovery")
	}
	for _, op := range d.operations {
		if op == "prepare" || op == "remove:key" {
			t.Fatal(d.operations)
		}
	}
	found := false
	for _, v := range r.Results {
		if v.Artifact.Kind == "inventory" && v.Status == Unverified {
			found = true
		}
	}
	if !found {
		t.Fatal(r)
	}
	if _, err = e.Check(context.Background(), false); err == nil {
		t.Fatal("full check certified empty primary scope")
	}
}
func TestHistoricalCompleteInventoryAllowsEmptyDiscoveryRetry(t *testing.T) {
	scope := Scope{Application: "app", Account: "123"}
	s := &memoryStore{r: &Report{Version: 1, Scope: scope, Targets: []Target{{ID: "app"}}, InventoryComplete: map[string]bool{"app": true}}}
	d := &fakeDriver{inventoryErr: map[string]error{"app": ErrAbsent}}
	if r, err := (&Engine{Scope: scope, Driver: d, Store: s, Recovery: []Artifact{{ID: "key", Kind: "recovery_key"}}}).Nuke(context.Background()); err != nil {
		t.Fatal(r, err)
	}
}
func TestTargetSharedAndDirectoryIdentityCannotChange(t *testing.T) {
	for _, target := range []Target{{ID: "app", Shared: true, Directory: "original"}, {ID: "app", Directory: "changed"}} {
		scope := Scope{Application: "app", Account: "123"}
		s := &memoryStore{r: &Report{Version: 1, Scope: scope, Targets: []Target{{ID: "app", Directory: "original"}}}}
		d := &fakeDriver{targets: []Target{target}}
		if _, err := (&Engine{Scope: scope, Driver: d, Store: s}).Nuke(context.Background()); err == nil {
			t.Fatal("target identity changed")
		}
		if !reflect.DeepEqual(d.operations, []string{"discover"}) {
			t.Fatal(d.operations)
		}
	}
}
func TestPartialDiscoveryContinuesKnownTargetsAndClearsOnRetry(t *testing.T) {
	scope := Scope{Application: "app", Account: "123"}
	s := &memoryStore{}
	issue := Artifact{ID: "project-error", Kind: "inventory", Data: map[string]string{"discovery": "true", "project": "other"}}
	d := &fakeDriver{targets: []Target{{ID: "shared", Shared: true}, {ID: "env"}}, discoverErr: &DiscoveryError{Artifacts: []Artifact{issue}}}
	e := &Engine{Scope: scope, Store: s, Driver: d, Recovery: []Artifact{{ID: "key", Kind: "recovery_key"}}}
	r, err := e.Nuke(context.Background())
	if err == nil {
		t.Fatal("partial discovery certified success")
	}
	var destroys []string
	for _, op := range d.operations {
		if op == "destroy:env" || op == "destroy:shared" {
			destroys = append(destroys, op)
		}
		if op == "remove:key" {
			t.Fatal("recovery removed despite partial discovery")
		}
	}
	if !reflect.DeepEqual(destroys, []string{"destroy:env", "destroy:shared"}) {
		t.Fatal(d.operations)
	}
	if r.Results[len(r.Results)-1].Status != Retained {
		t.Fatal(r.Results)
	}
	d.discoverErr = nil
	d.operations = nil
	r, err = e.Nuke(context.Background())
	if err != nil {
		t.Fatal(r, err)
	}
	for _, a := range r.Artifacts {
		if a.Data["discovery"] == "true" {
			t.Fatal("stale discovery marker")
		}
	}
}
