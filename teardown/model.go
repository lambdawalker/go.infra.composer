// Package teardown coordinates resumable, independently verified destruction.
package teardown

import (
	"context"
	"encoding/json"
	"errors"
)

type Scope struct {
	Application string `json:"application"`
	Account     string `json:"account"`
}
type Target struct {
	ID          string `json:"id"`
	Environment string `json:"environment"`
	Region      string `json:"region"`
	Backend     string `json:"backend"`
	Project     string `json:"project"`
	Stack       string `json:"stack"`
	Directory   string `json:"directory"`
	Shared      bool   `json:"shared"`
}
type Artifact struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Region   string            `json:"region"`
	TargetID string            `json:"target_id"`
	Recovery bool              `json:"recovery"`
	Data     map[string]string `json:"data,omitempty"`
}

func (a Artifact) Key() string {
	b, _ := json.Marshal([]string{a.Kind, a.Region, a.ID, a.TargetID})
	return string(b)
}

type Status string

const (
	Deleted    Status = "DELETED"
	Absent     Status = "ABSENT"
	Remains    Status = "REMAINS"
	Unverified Status = "UNVERIFIED"
	Failed     Status = "FAILED"
	Retained   Status = "RETAINED"
	Scheduled  Status = "SCHEDULED"
)

type State string

const (
	StateAbsent  State = "absent"
	StatePresent State = "present"
	StatePending State = "pending"
)

type Result struct {
	Artifact  Artifact `json:"artifact"`
	Status    Status   `json:"status"`
	Detail    string   `json:"detail,omitempty"`
	CheckedAt string   `json:"checked_at,omitempty"`
}
type Report struct {
	Version           int             `json:"version"`
	InventoryComplete map[string]bool `json:"inventory_complete,omitempty"`
	Scope             Scope           `json:"scope"`
	Targets           []Target        `json:"targets"`
	Artifacts         []Artifact      `json:"artifacts"`
	Results           []Result        `json:"results"`
	Checks            []Result        `json:"checks"`
	CheckScope        string          `json:"check_scope,omitempty"`
	CheckedAt         string          `json:"checked_at,omitempty"`
	UpdatedAt         string          `json:"updated_at"`
}

// DiscoveryError reports independent listing failures while preserving known targets.
type DiscoveryError struct{ Artifacts []Artifact }

func (*DiscoveryError) Error() string { return "target discovery incomplete" }

type Driver interface {
	Discover(context.Context, Scope) ([]Target, error)
	Inventory(context.Context, Target) ([]Artifact, error)
	Destroy(context.Context, Target) error
	Inspect(context.Context, Artifact) (State, error)
	RemoveRecovery(context.Context, Artifact) error
}

// ArtifactCleaner removes explicitly marked residual artifacts after stack destruction.
type ArtifactCleaner interface {
	Cleanup(context.Context, Artifact) error
}
type RecoveryPreflight interface {
	PrepareRecovery(context.Context, []Artifact) error
}
type Store interface {
	Load() (*Report, error)
	Save(*Report) error
}

var ErrAbsent = errors.New("target absent")
var ErrIncomplete = errors.New("teardown has remaining or unverified artifacts")

type Engine struct {
	Scope    Scope
	Driver   Driver
	Store    Store
	Recovery []Artifact
}
