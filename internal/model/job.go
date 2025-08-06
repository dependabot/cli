package model

import "time"

/*
Updating Models

If you are adding a new attribute to any of the models, a good rule of thumb is to add it was `yaml:"my-attribute,omitempty"`
initially _before_ you make any changes to core.

That will allow the CLI and our smoke tests to work with version of core before and after the change. Once you've released
the change, consider revisiting to remove `omitempty`, but be aware you will need to update all smoke tests to expect the
new key in their results.

When removing an attribute, the opposite is true:
- make it `omitempty` if it isn't already
- update Core
- update the smoke tests
- finally, remove it from the CLI

Finally, if you need to add a key for experimental features, please ensure:
- it is omitempty
- it is not added to payloads in core if the experiment is disabled

This will avoid churn where the experimental key makes it into other, unrelated smoke-tests as they are updated for other
reasons.
*/

// Job is the data that is passed to the updater.
type Job struct {
	PackageManager             string            `json:"package-manager" yaml:"package-manager"`
	AllowedUpdates             []Allowed         `json:"allowed-updates" yaml:"allowed-updates,omitempty"`
	Debug                      bool              `json:"debug" yaml:"debug,omitempty"`
	DependencyGroups           []Group           `json:"dependency-groups" yaml:"dependency-groups,omitempty"`
	Dependencies               []string          `json:"dependencies" yaml:"dependencies,omitempty"`
	DependencyGroupToRefresh   *string           `json:"dependency-group-to-refresh" yaml:"dependency-group-to-refresh,omitempty"`
	ExistingPullRequests       [][]ExistingPR    `json:"existing-pull-requests" yaml:"existing-pull-requests,omitempty"`
	ExistingGroupPullRequests  []ExistingGroupPR `json:"existing-group-pull-requests" yaml:"existing-group-pull-requests,omitempty"`
	Experiments                Experiment        `json:"experiments" yaml:"experiments,omitempty"`
	IgnoreConditions           []Condition       `json:"ignore-conditions" yaml:"ignore-conditions,omitempty"`
	LockfileOnly               bool              `json:"lockfile-only" yaml:"lockfile-only,omitempty"`
	RequirementsUpdateStrategy *string           `json:"requirements-update-strategy" yaml:"requirements-update-strategy,omitempty"`
	SecurityAdvisories         []Advisory        `json:"security-advisories" yaml:"security-advisories,omitempty"`
	SecurityUpdatesOnly        bool              `json:"security-updates-only" yaml:"security-updates-only,omitempty"`
	Source                     Source            `json:"source" yaml:"source"`
	UpdateSubdependencies      bool              `json:"update-subdependencies" yaml:"update-subdependencies,omitempty"`
	UpdatingAPullRequest       bool              `json:"updating-a-pull-request" yaml:"updating-a-pull-request,omitempty"`
	VendorDependencies         bool              `json:"vendor-dependencies" yaml:"vendor-dependencies,omitempty"`
	RejectExternalCode         bool              `json:"reject-external-code" yaml:"reject-external-code,omitempty"`
	RepoPrivate                bool              `json:"repo-private" yaml:"repo-private,omitempty"`
	CommitMessageOptions       *CommitOptions    `json:"commit-message-options" yaml:"commit-message-options,omitempty"`
	CredentialsMetadata        []Credential      `json:"credentials-metadata" yaml:"-"`
	MaxUpdaterRunTime          int               `json:"max-updater-run-time" yaml:"max-updater-run-time,omitempty"`
	UpdateCooldown             *UpdateCooldown   `json:"cooldown,omitempty" yaml:"cooldown,omitempty"`
	ExcludePaths               []string          `json:"exclude-paths" yaml:"exclude-paths,omitempty"`
}

func (j *Job) UseCaseInsensitiveFileSystem() bool {
	if experimentValue, isBoolean := j.Experiments["use_case_insensitive_filesystem"].(bool); isBoolean && experimentValue {
		return true
	}

	return false
}

// Source is a reference to some source code
type Source struct {
	Provider    string   `json:"provider" yaml:"provider,omitempty"`
	Repo        string   `json:"repo" yaml:"repo,omitempty"`
	Directory   string   `json:"directory,omitempty" yaml:"directory,omitempty"`
	Directories []string `json:"directories,omitempty" yaml:"directories,omitempty"`
	Branch      string   `json:"branch,omitempty" yaml:"branch,omitempty"`
	Commit      string   `json:"commit,omitempty" yaml:"commit,omitempty"`

	Hostname    *string `json:"hostname" yaml:"hostname,omitempty"`         // Must be provided if APIEndpoint is
	APIEndpoint *string `json:"api-endpoint" yaml:"api-endpoint,omitempty"` // Must be provided if Hostname is
}

type ExistingPR struct {
	DependencyName    string  `json:"dependency-name" yaml:"dependency-name"`
	DependencyVersion string  `json:"dependency-version" yaml:"dependency-version"`
	Directory         *string `json:"directory,omitempty" yaml:"directory,omitempty"`
}

type ExistingGroupPR struct {
	DependencyGroupName string       `json:"dependency-group-name" yaml:"dependency-group-name"`
	Dependencies        []ExistingPR `json:"dependencies" yaml:"dependencies"`
}

type Allowed struct {
	DependencyType string `json:"dependency-type,omitempty" yaml:"dependency-type,omitempty"`
	DependencyName string `json:"dependency-name,omitempty" yaml:"dependency-name,omitempty"`
	UpdateType     string `json:"update-type,omitempty" yaml:"update-type,omitempty"`
}

type Group struct {
	GroupName string         `json:"name,omitempty" yaml:"name,omitempty"`
	AppliesTo *string        `json:"applies-to,omitempty" yaml:"applies-to,omitempty"`
	Rules     map[string]any `json:"rules,omitempty" yaml:"rules,omitempty"`
}

type Condition struct {
	DependencyName     string     `json:"dependency-name" yaml:"dependency-name"`
	Source             string     `json:"source,omitempty" yaml:"source,omitempty"`
	UpdateTypes        []string   `json:"update-types,omitempty" yaml:"update-types,omitempty"`
	UpdatedAt          *time.Time `json:"updated-at,omitempty" yaml:"updated-at,omitempty"`
	VersionRequirement string     `json:"version-requirement,omitempty" yaml:"version-requirement,omitempty"`
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
	Directory            *string        `json:"directory,omitempty" yaml:"directory,omitempty"`
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
	Prefix            string `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	PrefixDevelopment string `json:"prefix-development,omitempty" yaml:"prefix-development,omitempty"`
	IncludeScope      bool   `json:"include-scope,omitempty" yaml:"include-scope,omitempty"`
}

type Credential map[string]any

type UpdateCooldown struct {
	DefaultDays     int      `json:"default-days,omitempty" yaml:"default-days,omitempty"`
	SemverMajorDays int      `json:"semver-major-days,omitempty" yaml:"semver-major-days,omitempty"`
	SemverMinorDays int      `json:"semver-minor-days,omitempty" yaml:"semver-minor-days,omitempty"`
	SemverPatchDays int      `json:"semver-patch-days,omitempty" yaml:"semver-patch-days,omitempty"`
	Include         []string `json:"include,omitempty" yaml:"include,omitempty"`
	Exclude         []string `json:"exclude,omitempty" yaml:"exclude,omitempty"`
}
