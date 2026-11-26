// Package awsicons provides the AWS service artwork used by Infra Composer.
package awsicons

import "embed"

//go:embed icons/aws/*.svg models/aws/*.glb aws-icons.js
var assets embed.FS

// Files returns the bundled service icons (icons/aws/<service>.svg), 3D models
// (models/aws/<service>.glb), and the browser resource-type mapper (aws-icons.js).
func Files() embed.FS {
	return assets
}
