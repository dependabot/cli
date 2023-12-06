package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_NewAzureRepo(t *testing.T) {
	tests := []struct {
		name           string
		packageManager string
		repo           string
		directory      string
		expected       *AzureRepo
	}{
		{
			name:           "valid repo",
			packageManager: "npm_and_yarn",
			repo:           "my-org/my-project/my-repo",
			directory:      "/",
			expected: &AzureRepo{
				PackageManger: "npm_and_yarn",
				Org:           "my-org",
				Project:       "my-project",
				Repo:          "my-repo",
				Directory:     "/",
			},
		},
		{
			name:           "invalid repo",
			packageManager: "npm_and_yarn",
			repo:           "my-org/my-project",
			directory:      "/",
			expected:       nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := NewAzureRepo(test.packageManager, test.repo, test.directory)
			assert.Equal(t, test.expected, actual)
		})
	}
}
