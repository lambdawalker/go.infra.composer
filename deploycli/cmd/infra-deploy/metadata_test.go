package main

import "testing"

func TestStateMetadata(t *testing.T) {
	tags := map[string]string{"Owner": "operator"}
	if !mergeStateTags(tags, "demo", "qa") {
		t.Fatal("initial metadata unchanged")
	}
	if tags["Name"] != "demo-qa" || tags["Project"] != "demo" || tags["Environment"] != "qa" || tags["Owner"] != "operator" {
		t.Fatal(tags)
	}
	if mergeStateTags(tags, "demo", "qa") {
		t.Fatal("unchanged metadata triggers write")
	}
	if !mergeStateTags(tags, "demo", "dev") || tags["Name"] != "demo-dev" {
		t.Fatal("environment change missing")
	}
}
