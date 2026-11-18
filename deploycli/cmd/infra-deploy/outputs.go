package main

import "encoding/json"

// Never persist arbitrary stack outputs: external configuration can add secrets.
func safeOutputs(raw []byte, allowlist []string) ([]byte, error) {
	var values map[string]json.RawMessage
	if e := json.Unmarshal(raw, &values); e != nil {
		return nil, e
	}
	safe := map[string]string{}
	for _, k := range allowlist {
		var value string
		if v, ok := values[k]; ok && json.Unmarshal(v, &value) == nil {
			safe[k] = value
		}
	}
	return json.MarshalIndent(safe, "", "  ")
}
