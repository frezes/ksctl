package cmd

import (
	"fmt"
	"runtime"
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

type VersionInfo struct {
	Version   string
	Commit    string
	BuildDate string
	GoVersion string
}

func DefaultVersionInfo() VersionInfo {
	return VersionInfo{
		Version:   version,
		Commit:    commit,
		BuildDate: buildDate,
		GoVersion: runtime.Version(),
	}
}

func (v VersionInfo) PrintHuman() string {
	if v.GoVersion == "" {
		v.GoVersion = runtime.Version()
	}
	return fmt.Sprintf("Client Version: %s\nGit Commit: %s\nBuild Date: %s\nGo Version: %s\n", v.Version, v.Commit, v.BuildDate, v.GoVersion)
}
