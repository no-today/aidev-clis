// Package buildinfo carries the version reported by --version. The project
// currently publishes NO releases (rolling main, installed via make install),
// so the version is a hardcoded calendar placeholder ("<year>-pre"; date-based
// like 2026.7.3-pre once releases resume). A release build stamps it via
// -ldflags -X (see .goreleaser.yaml), appending the short commit hash.
package buildinfo

var Version = "2026-pre"
