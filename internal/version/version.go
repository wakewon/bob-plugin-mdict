// Package version carries build-time identity for the bob-mdict service.
package version

// Version is the service version. Overridden at build time via -ldflags.
var Version = "0.1.2"

// Commit is the git commit the binary was built from. Overridden via -ldflags.
var Commit = "dev"

// APIVersion is the HTTP API contract version. The Bob plugin refuses to talk
// to a service advertising a different value, so bump it only on breaking
// changes to the request/response shapes under the current route major.
const APIVersion = "v2"

// DefaultPort is the loopback port the service listens on by default.
const DefaultPort = 15321
