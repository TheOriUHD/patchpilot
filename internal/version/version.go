package version

// Version is the semantic version baked into release builds.
var Version = "dev"

// Commit is the source control revision baked into release builds.
var Commit = "unknown"

// BuildDate is the UTC build timestamp baked into release builds.
var BuildDate = "unknown"

// BuildInfo describes the running patchpilot binary.
type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
}

// Info returns version metadata for the running binary.
func Info() BuildInfo {
	return BuildInfo{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
	}
}
