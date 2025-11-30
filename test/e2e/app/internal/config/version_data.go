package config

import (
	_ "embed"
)

//go:embed version_data.txt
var buildInfoData string

func GetVersionData() string {
	return buildInfoData
}
