// Package buildinfo holds the version of this build. Release builds set
// Version with -ldflags "-X github.com/abyssdblabs/abyssdb/internal/buildinfo.Version=…".
package buildinfo

// Version is the AbyssDB version, without a leading "v".
var Version = "0.0.0-null"

// Codename is the release codename.
const Codename = "Event Horizon"

// String is the version as AbyssDB prints it.
func String() string { return Version + ` (codename: "` + Codename + `")` }
