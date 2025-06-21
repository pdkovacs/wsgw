package config

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
)

//go:embed buildinfo.txt
var buildInfoString string

// VersionInfo holds information about the application's version
type VersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"buildTime"`
}

// BuildInfo holds basic information about the application
type BuildInfo struct {
	VersionInfo    VersionInfo `json:"versionInfo"`
	AppDescription string      `json:"appDescription"`
}

func getBuildProperty(key string) string {
	lines := strings.Split(string(buildInfoString), "\n")
	for _, line := range lines {
		fmt.Fprintf(os.Stderr, "key: %s, line: %#v\n", key, line)
		if strings.Index(line, key) == 0 {
			return strings.Split(line, "=")[1]
		}
	}
	return ""
}

var buildInfo BuildInfo

// GetBuildInfo returns basic information about the application
func GetBuildInfo() BuildInfo {
	if len(buildInfo.VersionInfo.Version) > 0 {
		return buildInfo
	}

	version := getBuildProperty("VERSION")
	commit := getBuildProperty("COMMIT")
	time := getBuildProperty("TIME")

	buildInfo = BuildInfo{
		VersionInfo: VersionInfo{
			Version:   version,
			Commit:    commit,
			BuildTime: time,
		},
		AppDescription: "Icon Repository",
	}

	fmt.Fprintf(os.Stderr, "buildInfo: %#v", buildInfo)

	return buildInfo
}

// GetBuildInfoString constructs and returns a string containing the build info.
func GetBuildInfoString() string {
	GetBuildInfo()
	return fmt.Sprintf("Version:\t%v\nCommit:\t\t%v\nBuild time:\t%v\n", buildInfo.VersionInfo.Version, buildInfo.VersionInfo.Commit, buildInfo.VersionInfo.BuildTime)
}
