// Package awsicons provides the AWS service artwork used by Infra Composer.
package awsicons

import "embed"

//go:embed icons/*/*.svg models/*/*.glb aws-icons.js
var assets embed.FS

// Files returns the bundled provider icons, 3D models, and browser resource-type mapper.
func Files() embed.FS {
	return assets
}
