package cmd

import (
	"fmt"
	"regexp"
	"runtime/debug"
)

// ldflags inserts the version here on release
var version string

var timestampRegex = regexp.MustCompile("[^a-zA-Z0-9]+")

func Version() string {
	if version == "" {
		version = "0.0.0-dev"
		commit := ""
		timestamp := ""
		modified := false

		info, _ := debug.ReadBuildInfo()
		for _, entry := range info.Settings {
			if entry.Key == "vcs.revision" && len(entry.Value) >= 7 {
				commit = entry.Value[:7] // short ref
			}

			if entry.Key == "vcs.modified" {
				modified = entry.Value == "true"
			}

			if entry.Key == "vcs.time" {
				timestamp = timestampRegex.ReplaceAllString(entry.Value, "")
			}
		}

		if modified && timestamp != "" {
			return fmt.Sprintf("%s+%s", version, timestamp)
		} else if commit != "" {
			return fmt.Sprintf("%s+%s", version, commit)
		}
	}

	return version
}
