package infra

import (
	"strings"
	"testing"
)

func TestTempDir(t *testing.T) {
	tmp := TempDir("tmp")
	if !strings.HasPrefix(tmp, "/") {
		t.Errorf("TempDir() = %v, want absolute path", tmp)
	}

	tmp = TempDir("/tmp/testing")
	if tmp != "/tmp/testing" {
		t.Errorf("TempDir() = %v, want /tmp/testing", tmp)
	}
}
