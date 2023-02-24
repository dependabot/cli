package model

type UpdateWrapper struct {
	Data any `json:"data" yaml:"data"`
}

type UpdateDependencyList struct {
	Dependencies    []Dependency `json:"dependencies" yaml:"dependencies"`
	DependencyFiles []string     `json:"dependency_files" yaml:"dependency_files"`
}

type CreatePullRequest struct {
	BaseCommitSha          string           `json:"base-commit-sha" yaml:"base-commit-sha"`
	Dependencies           []Dependency     `json:"dependencies" yaml:"dependencies"`
	UpdatedDependencyFiles []DependencyFile `json:"updated-dependency-files" yaml:"updated-dependency-files"`
	PRTitle                string           `json:"pr-title" yaml:"pr-title,omitempty"`
	PRBody                 string           `json:"pr-body" yaml:"pr-body,omitempty"`
	CommitMessage          string           `json:"commit-message" yaml:"commit-message,omitempty"`
	GroupedUpdate          bool             `json:"grouped-update" yaml:"grouped-update"`
}

type UpdatePullRequest struct {
	BaseCommitSha          string           `json:"base-commit-sha" yaml:"base-commit-sha"`
	DependencyNames        []string         `json:"dependency-names" yaml:"dependency-names"`
	UpdatedDependencyFiles []DependencyFile `json:"updated-dependency-files" yaml:"updated-dependency-files"`
	PRTitle                string           `json:"pr-title" yaml:"pr-title,omitempty"`
	PRBody                 string           `json:"pr-body" yaml:"pr-body,omitempty"`
	CommitMessage          string           `json:"commit-message" yaml:"commit-message,omitempty"`
}

type DependencyFile struct {
	Content         string `json:"content" yaml:"content"`
	ContentEncoding string `json:"content_encoding" yaml:"content_encoding"`
	Deleted         bool   `json:"deleted" yaml:"deleted"`
	Directory       string `json:"directory" yaml:"directory"`
	Name            string `json:"name" yaml:"name"`
	Operation       string `json:"operation" yaml:"operation"`
	SupportFile     bool   `json:"support_file" yaml:"support_file"`
	Type            string `json:"type" yaml:"type"`
	Mode            string `json:"mode" yaml:"mode,omitempty"`
}

type ClosePullRequest struct {
	DependencyNames []string `json:"dependency-names" yaml:"dependency-names"`
	Reason          string   `json:"reason" yaml:"reason"`
}

type MarkAsProcessed struct {
	BaseCommitSha string `json:"base-commit-sha" yaml:"base-commit-sha"`
}

type RecordPackageManagerVersion struct {
	Ecosystem       string         `json:"ecosystem" yaml:"ecosystem"`
	PackageManagers map[string]any `json:"package-managers" yaml:"package-managers"`
}

type RecordUpdateJobError struct {
	ErrorType    string         `json:"error-type" yaml:"error-type"`
	ErrorDetails map[string]any `json:"error-details" yaml:"error-details"`
}
