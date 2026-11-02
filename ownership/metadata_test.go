package ownership

import (
	"strings"
	"testing"
)

func fixture() Metadata {
	return Metadata{Application: "attestra", Environment: "dev", Domain: "attestrabond.com", Feature: "verifyemail", Deployment: "attestra-dev"}
}
func TestOwnershipTagsAndDescription(t *testing.T) {
	m := fixture()
	tags, err := m.Tags("signup")
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 7 || tags["ic:feature"] != "verifyemail" || tags["ic:managed-by"] != "infra-composer" {
		t.Fatal(tags)
	}
	tags["ic:application"] = "changed"
	again, _ := m.Tags("signup")
	if again["ic:application"] != "attestra" {
		t.Fatal("mutable metadata")
	}
	d, err := m.Description("Start email verification", 256)
	if err != nil || d != "[dev]/[attestrabond.com] attestra/verifyemail - Start email verification" {
		t.Fatal(d, err)
	}
	twice, err := m.Description(d, 256)
	if err != nil || twice != d {
		t.Fatal("description duplicated", twice, err)
	}
	m.Environment = "qa"
	d, err = m.Description(d, 256)
	if err != nil || strings.Contains(d, "[dev]") || !strings.HasPrefix(d, "[qa]") {
		t.Fatal("old ownership retained", d, err)
	}
	if _, err = m.Description(strings.Repeat("x", 256), 256); err == nil {
		t.Fatal("description silently truncated")
	}
}
func TestMissingAndUnsafeOwnershipRejected(t *testing.T) {
	for _, change := range []func(*Metadata){func(m *Metadata) { m.Application = "" }, func(m *Metadata) { m.Feature = "" }, func(m *Metadata) { m.Environment = "" }, func(m *Metadata) { m.Domain = "https://user:password@example.test/" }, func(m *Metadata) { m.Domain = "example.test\nforged" }, func(m *Metadata) { m.Deployment = "" }} {
		m := fixture()
		change(&m)
		if _, err := m.Tags("signup"); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	m := fixture()
	m.Environment = "shared"
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Tags(""); err == nil {
		t.Fatal("missing component accepted")
	}
}

func TestGeneratedPrefixMeetsIAMDescriptionCharset(t *testing.T) {
	value, err := fixture().Description("Email verification role", 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range value {
		if r > 255 {
			t.Fatal("generated description violates IAM ASCII/Latin-1 constraint")
		}
	}
}
