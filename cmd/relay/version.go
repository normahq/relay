package main

import (
	"fmt"
	buildinfo "runtime/debug"
	"strings"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func buildVersionString() string {
	resolvedVersion := strings.TrimSpace(version)
	if resolvedVersion == "" || resolvedVersion == "dev" {
		if info, ok := buildinfo.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			resolvedVersion = info.Main.Version
		}
	}
	if resolvedVersion == "" {
		resolvedVersion = "dev"
	}

	return fmt.Sprintf("relay %s (commit %s, built %s)", resolvedVersion, valueOrUnknown(commit), valueOrUnknown(date))
}

func valueOrUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}
