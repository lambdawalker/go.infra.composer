package main

import "testing"

func TestDestructivePlan(t *testing.T) {
	for _, tc := range []struct {
		data       string
		want, fail bool
	}{
		{`{"resourcePlans":{"urn":{"steps":["create"],"goal":{"deleteBeforeReplace":false,"policy":"delete"}}}}`, false, false},
		{`{"resourcePlans":{"urn":{"steps":["delete"]}}}`, true, false},
		{`{"resourcePlans":{"urn":{"steps":["create-replacement","delete-replaced"]}}}`, true, false},
		{`{"resourcePlans":{}}`, false, false}, {`{}`, false, true}, {`bad`, false, true},
	} {
		got, e := destructivePlan([]byte(tc.data))
		if got != tc.want || (e != nil) != tc.fail {
			t.Fatal(tc.data, got, e)
		}
	}
}
