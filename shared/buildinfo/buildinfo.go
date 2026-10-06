// Package buildinfo holds values injected into the binary at build time.
package buildinfo

// Commit is the git commit the binary was built from. It is set with -ldflags.
var Commit = "dev"
