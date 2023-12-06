package model

import "strings"

type AzureRepo struct {
	PackageManger string
	Org           string
	Project       string
	Repo          string
	Directory     string
}

// NewAzureRepo parses a repo string and returns an AzureRepo struct
// Expects a repo string in the format org/project/repo
func NewAzureRepo(packageManager string, repo string, directory string) *AzureRepo {
	repoParts := strings.Split(repo, "/")
	for i, part := range repoParts {
		println(i, part)
	}
	if len(repoParts) != 3 {
		return nil
	}

	return &AzureRepo{
		PackageManger: packageManager,
		Org:           repoParts[0],
		Project:       repoParts[1],
		Repo:          repoParts[2],
		Directory:     directory,
	}
}
