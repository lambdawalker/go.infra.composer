package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// One record per listener allows concurrent wizards to update their own entries.
// The instance ID is public metadata, separate from the private browser link.
type wizardInstance struct {
	Version    int       `json:"version"`
	InstanceID string    `json:"instanceId"`
	Repository string    `json:"repository"`
	Project    string    `json:"project,omitempty"`
	Port       int       `json:"port"`
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"startedAt"`
}

type wizardRegistrationKey struct{}

type wizardRegistration struct {
	path     string
	instance wizardInstance
}

func registerWizard(root, repository, requestedProject string, port int, instanceID string) (*wizardRegistration, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("locate wizard index: %w", err)
	}
	composerDir := filepath.Join(home, ".infra-composer")
	index := filepath.Join(composerDir, "_wizards")
	if err := privateDirectory(index); err != nil {
		return nil, fmt.Errorf("prepare wizard index: %w", err)
	}
	registration := &wizardRegistration{
		path: filepath.Join(index, instanceID+".json"),
		instance: wizardInstance{
			Version:    1,
			InstanceID: instanceID,
			Repository: repository,
			Project:    registeredProject(root, composerDir, requestedProject),
			Port:       port,
			PID:        os.Getpid(),
			StartedAt:  time.Now().UTC(),
		},
	}
	if err := registration.write(registration.instance); err != nil {
		return nil, err
	}
	return registration, nil
}

func (r *wizardRegistration) write(instance wizardInstance) error {
	data, err := json.MarshalIndent(instance, "", "  ")
	if err == nil {
		err = atomicWrite(r.path, append(data, '\n'))
	}
	if err != nil {
		return fmt.Errorf("write wizard index entry: %w", err)
	}
	r.instance = instance
	return nil
}

func (r *wizardRegistration) setProject(project string) error {
	if r.instance.Project == project {
		return nil
	}
	instance := r.instance
	instance.Project = project
	return r.write(instance)
}

func (r *wizardRegistration) remove() error {
	if err := os.Remove(r.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove wizard index entry: %w", err)
	}
	return nil
}
