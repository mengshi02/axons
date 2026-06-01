// Package version provides the global version information for axons.
// These variables are set at build time via -ldflags.
package version

var (
	// Version is the application version. Set via -ldflags "-X github.com/mengshi02/axons/internal/version.Version=v1.0.0".
	Version = "dev"
	// Commit is the git commit hash. Set via -ldflags.
	Commit = "none"
	// Date is the build date. Set via -ldflags.
	Date = "unknown"
)