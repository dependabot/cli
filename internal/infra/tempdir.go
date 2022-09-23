package infra

import (
	"os"
	"path"
)

// TempDir centralizes where the temporary directory is created.
func TempDir(tmpPath string) string {
	if path.IsAbs(tmpPath) {
		mkdirAll(tmpPath)
		return tmpPath
	}

	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	tmpPath = path.Join(wd, tmpPath)
	mkdirAll(tmpPath)
	return tmpPath
}

func mkdirAll(tmpPath string) {
	err := os.MkdirAll(tmpPath, 0700)
	if err != nil {
		panic(err)
	}
}
