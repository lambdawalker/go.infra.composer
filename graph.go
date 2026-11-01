package composer

// orderResources uses explicit DFS frames; long chains do not depend on recursion.
func orderResources(nodes map[string]PlannedResource) ([]string, error) {
	var ds []Diagnostic
	for _, id := range sortedKeys(nodes) {
		for _, e := range nodes[id].Dependencies {
			if _, ok := nodes[e.Resource]; !ok {
				ds = append(ds, diagnostic("IC_UNKNOWN_RESOURCE", id+" requires unknown "+e.Resource+".", e.Source, "Include the prerequisite's owning feature."))
			}
		}
	}
	if len(ds) > 0 {
		return nil, &ValidationError{Diagnostics: ds}
	}
	state := map[string]int{}
	ordered := make([]string, 0, len(nodes))
	type frame struct {
		id   string
		next int
	}
	for _, start := range sortedKeys(nodes) {
		if state[start] != 0 {
			continue
		}
		state[start] = 1
		frames := []frame{{id: start}}
		path := []string{start}
		positions := map[string]int{start: 0}
		for len(frames) > 0 {
			f := &frames[len(frames)-1]
			edges := nodes[f.id].Dependencies
			if f.next == len(edges) {
				state[f.id] = 2
				ordered = append(ordered, f.id)
				delete(positions, f.id)
				frames = frames[:len(frames)-1]
				path = path[:len(path)-1]
				continue
			}
			edge := edges[f.next]
			f.next++
			if state[edge.Resource] == 0 {
				state[edge.Resource] = 1
				positions[edge.Resource] = len(path)
				path = append(path, edge.Resource)
				frames = append(frames, frame{id: edge.Resource})
			} else if state[edge.Resource] == 1 {
				cycle := append(append([]string{}, path[positions[edge.Resource]:]...), edge.Resource)
				d := diagnostic("IC_DEPENDENCY_CYCLE", "No resource in this cycle can be created first.", edge.Source, "Remove a self-reference, separate a binding, or introduce an independent interface.")
				d.Path = cycle
				for i := 0; i < len(cycle)-1; i++ {
					for _, e := range nodes[cycle[i]].Dependencies {
						if e.Resource == cycle[i+1] {
							d.Details = append(d.Details, cycle[i]+" needs "+e.Resource+": "+e.Reason+" ("+e.Source+")")
							break
						}
					}
				}
				ds = append(ds, d)
			}
		}
	}
	if len(ds) > 0 {
		return nil, &ValidationError{Diagnostics: ds}
	}
	return ordered, nil
}
