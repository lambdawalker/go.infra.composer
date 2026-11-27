// Package awsicons provides the AWS service artwork used by Infra Composer.
package awsicons

import (
	"embed"
	"github.com/lambdawalker/go.infra.composer/internal/reportui"
)

// Files returns the bundled provider icons, 3D models, and browser resource-type mapper.
func Files() embed.FS { return reportui.Files() }
