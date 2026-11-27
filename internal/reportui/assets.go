// Package reportui holds the shared deployment workspace and report assets.
package reportui

import "embed"

//go:embed icons/*/*.svg models/*/*.glb aws-icons.js
var assets embed.FS

// Files returns the bundled provider icons, 3D models, and browser resource-type mapper.
func Files() embed.FS {
	return assets
}

//go:embed topology.js topology.css topology.html theme.css vendor/*
var UI embed.FS
