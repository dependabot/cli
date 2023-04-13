package model

// Job is the data that is passed to the updater.
type Job struct {
	PackageManager             string         `json:"package-manager" yaml:"package-manager"`
	AllowedUpdates             []Allowed      `json:"allowed-updates" yaml:"allowed-updates,omitempty"`
	DependencyGroups           []Group        `json:"dependency-groups" yaml:"dependency-groups,omitempty"`
	Dependencies               []string       `json:"dependencies" yaml:"dependencies,omitempty"`
	ExistingPullRequests       [][]ExistingPR `json:"existing-pull-requests" yaml:"existing-pull-requests,omitempty"`
	Experiments                Experiment     `json:"experiments" yaml:"experiments,omitempty"`
	IgnoreConditions           []Condition    `json:"ignore-conditions" yaml:"ignore-conditions,omitempty"`
	LockfileOnly               bool           `json:"lockfile-only" yaml:"lockfile-only,omitempty"`
	RequirementsUpdateStrategy *string        `json:"requirements-update-strategy" yaml:"requirements-update-strategy,omitempty"`
	SecurityAdvisories         []Advisory     `json:"security-advisories" yaml:"security-advisories,omitempty"`
	SecurityUpdatesOnly        bool           `json:"security-updates-only" yaml:"security-updates-only,omitempty"`
	Source                     Source         `json:"source" yaml:"source"`
	UpdateSubdependencies      bool           `json:"update-subdependencies" yaml:"update-subdependencies,omitempty"`
	UpdatingAPullRequest       bool           `json:"updating-a-pull-request" yaml:"updating-a-pull-request,omitempty"`
	VendorDependencies         bool           `json:"vendor-dependencies" yaml:"vendor-dependencies,omitempty"`
	RejectExternalCode         bool           `json:"reject-external-code" yaml:"reject-external-code,omitempty"`
	CommitMessageOptions       *CommitOptions `json:"commit-message-options" yaml:"commit-message-options,omitempty"`
	CredentialsMetadata        []Credential   `json:"credentials-metadata" yaml:"credentials-metadata,omitempty"`
	MaxUpdaterRunTime          int            `json:"max-updater-run-time" yaml:"max-updater-run-time,omitempty"`
}

// Source is a reference to some source code
type Source struct {
	Provider  string  `json:"provider" yaml:"provider,omitempty"`
	Repo      string  `json:"repo" yaml:"repo,omitempty"`
	Directory string  `json:"directory" yaml:"directory,omitempty"`
	Branch    *string `json:"branch" yaml:"branch,omitempty"`
	Commit    *string `json:"commit" yaml:"commit,omitempty"`

	Hostname    *string `json:"hostname" yaml:"hostname,omitempty"`         // Must be provided if APIEndpoint is
	APIEndpoint *string `json:"api-endpoint" yaml:"api-endpoint,omitempty"` // Must be provided if Hostname is
}

type ExistingPR struct {
	DependencyName    string `json:"dependency-name" yaml:"dependency-name"`
	DependencyVersion string `json:"dependency-version" yaml:"dependency-version"`
}

type Allowed struct {
	DependencyType string `json:"dependency-type,omitempty" yaml:"dependency-type,omitempty"`
	DependencyName string `json:"dependency-name,omitempty" yaml:"dependency-name,omitempty"`
	UpdateType     string `json:"update-type,omitempty" yaml:"update-type,omitempty"`
}

type Group struct {
	GroupName string         `json:"name,omitempty" yaml:"name,omitempty"`
	Rules     map[string]any `json:"rules,omitempty" yaml:"rules,omitempty"`
}

type Condition struct {
	DependencyName     string   `json:"dependency-name" yaml:"dependency-name"`
	Source             string   `json:"source,omitempty" yaml:"source,omitempty"`
	UpdateTypes        []string `json:"update-types,omitempty" yaml:"update-types,omitempty"`
	VersionRequirement string   `json:"version-requirement,omitempty" yaml:"version-requirement,omitempty"`
}

type Advisory struct {
	DependencyName     string   `json:"dependency-name" yaml:"dependency-name"`
	AffectedVersions   []string `json:"affected-versions" yaml:"affected-versions"`
	PatchedVersions    []string `json:"patched-versions" yaml:"patched-versions"`
	UnaffectedVersions []string `json:"unaffected-versions" yaml:"unaffected-versions"`
}

type Dependency struct {
	Name                 string         `json:"name"`
	PreviousRequirements *[]Requirement `json:"previous-requirements,omitempty" yaml:"previous-requirements,omitempty"`
	PreviousVersion      string         `json:"previous-version,omitempty" yaml:"previous-version,omitempty"`
	Requirements         []Requirement  `json:"requirements"`
	Version              *string        `json:"version" yaml:"version"`
	Removed              bool           `json:"removed,omitempty" yaml:"removed,omitempty"`
}

type Requirement struct {
	File            string             `json:"file" yaml:"file"`
	Groups          []any              `json:"groups" yaml:"groups"`
	Metadata        *map[string]any    `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Requirement     *string            `json:"requirement" yaml:"requirement"`
	Source          *RequirementSource `json:"source" yaml:"source"`
	Version         string             `json:"version,omitempty" yaml:"version,omitempty"`
	PreviousVersion string             `json:"previous-version,omitempty" yaml:"previous-version,omitempty"`
}

type RequirementSource map[string]any
type Experiment map[string]any

type CommitOptions struct {
	Prefix            string  `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	PrefixDevelopment string  `json:"prefix-development,omitempty" yaml:"prefix-development,omitempty"`
	IncludeScope      *string `json:"include-scope,omitempty" yaml:"include-scope,omitempty"`
}

type Credential map[string]any
