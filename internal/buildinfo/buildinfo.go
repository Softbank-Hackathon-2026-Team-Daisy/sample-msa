// Package buildinfo exposes the identity of the running artifact.
//
// Version, Commit and BuildTime may be overridden at link time, e.g.:
//
//	go build -ldflags "-X github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/buildinfo.Commit=abc123"
package buildinfo

import "runtime"

// Link-time overridable build metadata. Defaults keep un-injected builds valid.
var (
	Version   = "1.0.0"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Info describes the running build.
type Info struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"buildTime"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Get returns the build information of the current binary for the named
// service.
func Get(name string) Info {
	return Info{
		Name:      name,
		Version:   Version,
		Commit:    Commit,
		BuildTime: BuildTime,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}
