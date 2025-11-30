package config

import (
	_ "embed"
)

//go:embed version_data.txt
var versionData string

func GetVersionData() string {
	return versionData
}
