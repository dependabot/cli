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
	DependencyGroup        map[string]any   `json:"dependency-group" yaml:"dependency-group,omitempty"`
}

type UpdatePullRequest struct {
	BaseCommitSha          string           `json:"base-commit-sha" yaml:"base-commit-sha"`
	DependencyNames        []string         `json:"dependency-names" yaml:"dependency-names"`
	UpdatedDependencyFiles []DependencyFile `json:"updated-dependency-files" yaml:"updated-dependency-files"`
	PRTitle                string           `json:"pr-title" yaml:"pr-title,omitempty"`
	PRBody                 string           `json:"pr-body" yaml:"pr-body,omitempty"`
	CommitMessage          string           `json:"commit-message" yaml:"commit-message,omitempty"`
	DependencyGroup        map[string]any   `json:"dependency-group" yaml:"dependency-group,omitempty"`
}

type DependencyFile struct {
	Content         string `json:"content" yaml:"content"`
	ContentEncoding string `json:"content_encoding" yaml:"content_encoding"`
	Deleted         bool   `json:"deleted" yaml:"deleted"`
	Directory       string `json:"directory" yaml:"directory"`
	Name            string `json:"name" yaml:"name"`
	Operation       string `json:"operation" yaml:"operation"`
	SupportFile     bool   `json:"support_file" yaml:"support_file"`
	SymlinkTarget   string `json:"symlink_target,omitempty" yaml:"symlink_target,omitempty"`
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

type RecordEcosystemVersions struct {
	EcosystemVersions map[string]any `json:"ecosystem_versions" yaml:"ecosystem_versions"`
}

type RecordEcosystemMeta struct {
	Ecosystem Ecosystem `json:"ecosystem" yaml:"ecosystem"`
}

type RecordUpdateJobError struct {
	ErrorType    string         `json:"error-type" yaml:"error-type"`
	ErrorDetails map[string]any `json:"error-details" yaml:"error-details"`
}

type RecordUpdateJobUnknownError struct {
	ErrorType    string         `json:"error-type" yaml:"error-type"`
	ErrorDetails map[string]any `json:"error-details" yaml:"error-details"`
}

type IncrementMetric struct {
	Metric string         `json:"metric" yaml:"metric"`
	Tags   map[string]any `json:"tags" yaml:"tags"`
}

type Ecosystem struct {
	Name           string         `json:"name" yaml:"name"`
	PackageManager VersionManager `json:"package_manager,omitempty" yaml:"package_manager,omitempty"`
	Language       VersionManager `json:"language,omitempty" yaml:"language,omitempty"`
}

type VersionManager struct {
	Name        string         `json:"name" yaml:"name"`
	Version     string         `json:"version" yaml:"version"`
	RawVersion  string         `json:"raw_version" yaml:"raw_version"`
	Requirement map[string]any `json:"requirement,omitempty" yaml:"requirement,omitempty"`
}
