// Package version provides the global version information for axons.
// These variables are set at build time via -ldflags.
package version

var (
	// Version is the application version. Set via -ldflags "-X github.com/mengshi02/axons/internal/version.Version=v1.1.0".
	// The default "dev" is overridden at build time; the real version is read from the VERSION file by Makefile.
	Version = "dev"
	// Commit is the git commit hash. Set via -ldflags.
	Commit = "none"
	// Date is the build date. Set via -ldflags.
	Date = "unknown"
)