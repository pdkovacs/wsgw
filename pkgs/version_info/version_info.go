package version_info

import (
	"fmt"
	"os"
	"strings"
)

// VersionInfo holds information about the application's version
type VersionInfo struct {
	Application string `json:"application"`
	Version     string `json:"version"`
	Commit      string `json:"commit"`
	BuildTime   string `json:"buildTime"`
}

func getVersionProperty(versionData string, key string) string {
	lines := strings.SplitSeq(versionData, "\n")
	for line := range lines {
		fmt.Fprintf(os.Stderr, "key: %s, line: %#v\n", key, line)
		if strings.Index(line, key) == 0 {
			return strings.Split(line, "=")[1]
		}
	}
	return ""
}

// GetVersionInfo returns basic information about the application
func GetVersionInfo(versionData string) VersionInfo {
	application := getVersionProperty(versionData, "APPLICATION")
	version := getVersionProperty(versionData, "VERSION")
	commit := getVersionProperty(versionData, "COMMIT")
	time := getVersionProperty(versionData, "TIME")

	versionInfo := VersionInfo{
		Application: application,
		Version:     version,
		Commit:      commit,
		BuildTime:   time,
	}

	fmt.Fprintf(os.Stderr, "versionInfo: %#v", versionInfo)

	return versionInfo
}
