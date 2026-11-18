package main

// A normal interactive launcher invocation previews then offers timed apply.
// Explicit actions and noninteractive preview defaults never escalate to deployment.
func defaultAction(action string, noninteractive bool) string {
	if action != "" {
		return action
	}
	if noninteractive {
		return "preview"
	}
	return "deploy"
}
