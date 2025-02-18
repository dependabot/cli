package cmd

import (
	"log"
	"runtime/debug"
)

// ldflags inserts the version here on release
var version string

func Version() string {
	if version == "" {
		version = "0.0.0-dev"

		info, ok := debug.ReadBuildInfo()
		if !ok {
			log.Println("debug.ReadBuildInfo failed")
			return version
		}
		if info.Main.Version != "" {
			version = info.Main.Version
		}
	}

	return version
}
