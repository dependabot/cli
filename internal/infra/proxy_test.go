package infra

import (
	"testing"

	"github.com/docker/docker/pkg/namesgenerator"
)

func TestSeed(t *testing.T) {
	// ensure we're still seeding
	a := namesgenerator.GetRandomName(1)
	b := namesgenerator.GetRandomName(1)
	if a == b {
		t.Error("Not seeding math/rand")
	}
}
