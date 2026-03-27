package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestInput(t *testing.T) {
	var input Input
	if err := yaml.Unmarshal([]byte(exampleJob), &input); err != nil {
		t.Fatal(err)
	}
	var input2 map[string]map[string]any
	if err := yaml.Unmarshal([]byte(exampleJob), &input2); err != nil {
		t.Fatal(err)
	}
	compareMap(t, "job", input2["job"], input.Job)
}

func TestAllowedUpdateTypes(t *testing.T) {
	var input Input
	if err := yaml.Unmarshal([]byte(exampleJob), &input); err != nil {
		t.Fatal(err)
	}

	allowed := input.Job.AllowedUpdates
	if len(allowed) != 2 {
		t.Fatalf("expected 2 allowed updates, got %d", len(allowed))
	}

	// First entry: dependency-type + update-type (existing pattern)
	if allowed[0].DependencyType != "direct" {
		t.Errorf("expected dependency-type 'direct', got %q", allowed[0].DependencyType)
	}
	if allowed[0].UpdateType != "all" {
		t.Errorf("expected update-type 'all', got %q", allowed[0].UpdateType)
	}
	if len(allowed[0].UpdateTypes) != 0 {
		t.Errorf("expected no update-types on first entry, got %v", allowed[0].UpdateTypes)
	}

	// Second entry: dependency-name + update-types (new feature)
	if allowed[1].DependencyName != "rails" {
		t.Errorf("expected dependency-name 'rails', got %q", allowed[1].DependencyName)
	}
	expectedTypes := []string{"version-update:semver-minor", "version-update:semver-patch"}
	if len(allowed[1].UpdateTypes) != len(expectedTypes) {
		t.Fatalf("expected %d update-types, got %d", len(expectedTypes), len(allowed[1].UpdateTypes))
	}
	for i, et := range expectedTypes {
		if allowed[1].UpdateTypes[i] != et {
			t.Errorf("update-types[%d]: expected %q, got %q", i, et, allowed[1].UpdateTypes[i])
		}
	}
}

func TestAllowedUpdateTypesJSON(t *testing.T) {
	original := Allowed{
		DependencyName: "rails",
		UpdateTypes:    []string{"version-update:semver-minor", "version-update:semver-patch"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	// Verify marshaled JSON directly without using unmarshal
	expected := `{"dependency-name":"rails","update-types":["version-update:semver-minor","version-update:semver-patch"]}`
	if string(data) != expected {
		t.Errorf("unexpected JSON output:\n  got:  %s\n  want: %s", string(data), expected)
	}

	// Verify omitempty: UpdateTypes should be absent when nil
	empty := Allowed{DependencyName: "rails"}
	data, err = json.Marshal(empty)
	if err != nil {
		t.Fatal(err)
	}

	expectedEmpty := `{"dependency-name":"rails"}`
	if string(data) != expectedEmpty {
		t.Errorf("unexpected JSON output for empty UpdateTypes:\n  got:  %s\n  want: %s", string(data), expectedEmpty)
	}
}

func TestExistingPullRequestsNewFormat(t *testing.T) {
	testYAML := `---
job:
  package-manager: npm_and_yarn
  source:
    provider: github
    repo: dependabot/test
    directory: "/"
  existing-pull-requests:
    - pr-number: 123
      dependencies:
        - dependency-name: dependency-a
          dependency-version: 1.2.5
          directory: "/"
        - dependency-name: dependency-b
          dependency-version: 2.0.0
          directory: "/sub"
    - pr-number: 456
      dependencies:
        - dependency-name: dependency-c
          dependency-version: 3.1.0
    - dependency-name: dependency-d
      dependency-version: 4.0.0
      directory: "/"
`

	var input Input
	if err := yaml.Unmarshal([]byte(testYAML), &input); err != nil {
		t.Fatal(err)
	}

	prs := input.Job.ExistingPullRequests
	if len(prs) != 3 {
		t.Fatalf("expected 3 PR entries, got %d", len(prs))
	}

	// Test first grouped PR with pr-number 123
	if prs[0].PRNumber == nil || *prs[0].PRNumber != 123 {
		t.Errorf("expected pr-number 123, got %v", prs[0].PRNumber)
	}
	if prs[0].Dependencies == nil || len(*prs[0].Dependencies) != 2 {
		t.Fatalf("expected 2 dependencies in PR 123, got %v", prs[0].Dependencies)
	}
	if (*prs[0].Dependencies)[0].DependencyName != "dependency-a" {
		t.Errorf("expected dependency-a, got %s", (*prs[0].Dependencies)[0].DependencyName)
	}
	if (*prs[0].Dependencies)[0].DependencyVersion != "1.2.5" {
		t.Errorf("expected version 1.2.5, got %s", (*prs[0].Dependencies)[0].DependencyVersion)
	}
	if (*prs[0].Dependencies)[1].DependencyName != "dependency-b" {
		t.Errorf("expected dependency-b, got %s", (*prs[0].Dependencies)[1].DependencyName)
	}

	// Test second grouped PR with pr-number 456
	if prs[1].PRNumber == nil || *prs[1].PRNumber != 456 {
		t.Errorf("expected pr-number 456, got %v", prs[1].PRNumber)
	}
	if prs[1].Dependencies == nil || len(*prs[1].Dependencies) != 1 {
		t.Fatalf("expected 1 dependency in PR 456, got %v", prs[1].Dependencies)
	}

	// Test flat format entry (no pr-number, direct dependency fields)
	if prs[2].DependencyName != "dependency-d" {
		t.Errorf("expected dependency-d, got %s", prs[2].DependencyName)
	}
	if prs[2].DependencyVersion != "4.0.0" {
		t.Errorf("expected version 4.0.0, got %s", prs[2].DependencyVersion)
	}
	if prs[2].Dependencies != nil {
		t.Errorf("expected no nested dependencies for flat format entry")
	}
}

func TestExistingPullRequestsNewFormatJSON(t *testing.T) {
	testJSON := `{
  "job": {
    "package-manager": "npm_and_yarn",
    "source": {
      "provider": "github",
      "repo": "dependabot/test",
      "directory": "/"
    },
    "existing-pull-requests": [
      {
        "pr-number": 123,
        "dependencies": [
          {
            "dependency-name": "dependency-a",
            "dependency-version": "1.2.5",
            "directory": "/"
          },
          {
            "dependency-name": "dependency-b",
            "dependency-version": "2.0.0",
            "directory": "/sub"
          }
        ]
      },
      {
        "pr-number": 456,
        "dependencies": [
          {
            "dependency-name": "dependency-c",
            "dependency-version": "3.1.0"
          }
        ]
      },
      {
        "dependency-name": "dependency-d",
        "dependency-version": "4.0.0",
        "directory": "/"
      }
    ]
  }
}`

	var input Input
	if err := json.Unmarshal([]byte(testJSON), &input); err != nil {
		t.Fatal(err)
	}

	prs := input.Job.ExistingPullRequests
	if len(prs) != 3 {
		t.Fatalf("expected 3 PR entries, got %d", len(prs))
	}

	// Test first grouped PR with pr-number 123
	if prs[0].PRNumber == nil || *prs[0].PRNumber != 123 {
		t.Errorf("expected pr-number 123, got %v", prs[0].PRNumber)
	}
	if prs[0].Dependencies == nil || len(*prs[0].Dependencies) != 2 {
		t.Fatalf("expected 2 dependencies in PR 123, got %v", prs[0].Dependencies)
	}
	if (*prs[0].Dependencies)[0].DependencyName != "dependency-a" {
		t.Errorf("expected dependency-a, got %s", (*prs[0].Dependencies)[0].DependencyName)
	}

	// Test second grouped PR with pr-number 456
	if prs[1].PRNumber == nil || *prs[1].PRNumber != 456 {
		t.Errorf("expected pr-number 456, got %v", prs[1].PRNumber)
	}

	// Test flat format entry (no pr-number, direct dependency fields)
	if prs[2].DependencyName != "dependency-d" {
		t.Errorf("expected dependency-d, got %s", prs[2].DependencyName)
	}
}

func TestExistingPullRequestsOldFormat(t *testing.T) {
	testYAML := `---
job:
  package-manager: go_modules
  source:
    provider: github
    repo: dependabot/test
    directory: "/"
  existing-pull-requests:
    - - dependency-name: dep-x
        dependency-version: 1.0.0
    - - dependency-name: dep-y
        dependency-version: 2.0.0
`

	var input Input
	if err := yaml.Unmarshal([]byte(testYAML), &input); err != nil {
		t.Fatal(err)
	}

	prs := input.Job.ExistingPullRequests
	if len(prs) != 2 {
		t.Fatalf("expected 2 PR entries from old format, got %d", len(prs))
	}

	if prs[0].DependencyName != "dep-x" {
		t.Errorf("expected dep-x, got %s", prs[0].DependencyName)
	}
	if prs[0].DependencyVersion != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", prs[0].DependencyVersion)
	}

	if prs[1].DependencyName != "dep-y" {
		t.Errorf("expected dep-y, got %s", prs[1].DependencyName)
	}
}

func TestExistingPullRequestsOldFormatJSON(t *testing.T) {
	testJSON := `{
  "job": {
    "package-manager": "go_modules",
    "source": {
      "provider": "github",
      "repo": "dependabot/test",
      "directory": "/"
    },
    "existing-pull-requests": [
      [
        {
          "dependency-name": "dep-x",
          "dependency-version": "1.0.0"
        }
      ],
      [
        {
          "dependency-name": "dep-y",
          "dependency-version": "2.0.0"
        }
      ]
    ]
  }
}`

	var input Input
	if err := json.Unmarshal([]byte(testJSON), &input); err != nil {
		t.Fatal(err)
	}

	prs := input.Job.ExistingPullRequests
	if len(prs) != 2 {
		t.Fatalf("expected 2 PR entries from old format, got %d", len(prs))
	}

	if prs[0].DependencyName != "dep-x" {
		t.Errorf("expected dep-x, got %s", prs[0].DependencyName)
	}
	if prs[0].DependencyVersion != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", prs[0].DependencyVersion)
	}

	if prs[1].DependencyName != "dep-y" {
		t.Errorf("expected dep-y, got %s", prs[1].DependencyName)
	}
	if prs[1].DependencyVersion != "2.0.0" {
		t.Errorf("expected version 2.0.0, got %s", prs[1].DependencyVersion)
	}
}

func TestExistingPullRequestsDependencyRemoved(t *testing.T) {
	testYAML := `---
job:
  package-manager: npm_and_yarn
  source:
    provider: github
    repo: dependabot/test
    directory: "/"
  existing-pull-requests:
    - pr-number: 42
      dependencies:
        - dependency-name: antd
          dependency-version: 6.3.2
        - dependency-name: node-fetch
          dependency-removed: true
`

	var input Input
	if err := yaml.Unmarshal([]byte(testYAML), &input); err != nil {
		t.Fatal(err)
	}

	prs := input.Job.ExistingPullRequests
	if len(prs) != 1 {
		t.Fatalf("expected 1 PR entry, got %d", len(prs))
	}

	if prs[0].PRNumber == nil || *prs[0].PRNumber != 42 {
		t.Errorf("expected pr-number 42, got %v", prs[0].PRNumber)
	}
	if prs[0].Dependencies == nil || len(*prs[0].Dependencies) != 2 {
		t.Fatalf("expected 2 dependencies in PR 42, got %v", prs[0].Dependencies)
	}

	deps := *prs[0].Dependencies
	if deps[0].DependencyName != "antd" {
		t.Errorf("expected antd, got %s", deps[0].DependencyName)
	}
	if deps[0].DependencyVersion != "6.3.2" {
		t.Errorf("expected version 6.3.2, got %s", deps[0].DependencyVersion)
	}
	if deps[0].DependencyRemoved {
		t.Error("expected antd dependency-removed to be false")
	}

	if deps[1].DependencyName != "node-fetch" {
		t.Errorf("expected node-fetch, got %s", deps[1].DependencyName)
	}
	if !deps[1].DependencyRemoved {
		t.Error("expected node-fetch dependency-removed to be true")
	}
	if deps[1].DependencyVersion != "" {
		t.Errorf("expected no version for removed dep, got %s", deps[1].DependencyVersion)
	}

	// Verify round-trip: marshal to JSON and check dependency-removed is preserved
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	jsonStr := string(data)

	if !strings.Contains(jsonStr, `"dependency-removed":true`) {
		t.Errorf("JSON output missing dependency-removed:true: %s", jsonStr)
	}

	// The node-fetch entry should not have dependency-version
	nodeFetchIdx := strings.Index(jsonStr, `"dependency-name":"node-fetch"`)
	if nodeFetchIdx < 0 {
		t.Fatalf("JSON output missing node-fetch entry: %s", jsonStr)
	}
	nodeFetchObj := jsonStr[nodeFetchIdx:]
	closeBrace := strings.Index(nodeFetchObj, "}")
	nodeFetchObj = nodeFetchObj[:closeBrace]
	if strings.Contains(nodeFetchObj, `"dependency-version"`) {
		t.Errorf("node-fetch should not have dependency-version when dependency-removed is true: %s", jsonStr)
	}

	var roundTripped Input
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("failed to round-trip JSON: %v", err)
	}

	rtPRs := roundTripped.Job.ExistingPullRequests
	if len(rtPRs) != 1 {
		t.Fatalf("round-trip: expected 1 PR entry, got %d", len(rtPRs))
	}
	rtDeps := *rtPRs[0].Dependencies
	if !rtDeps[1].DependencyRemoved {
		t.Errorf("round-trip: dependency-removed lost in JSON: %s", jsonStr)
	}
}

func compareMap(t *testing.T, parent string, expected map[string]any, actual interface{}) {
	actualType := reflect.TypeOf(actual)
	if actualType.Kind() != reflect.Struct {
		// Some fields like Experiments are not modeled as structs
		// so there's nothing to do here!
		return
	}
	fields := actualType.NumField()
	for key, value := range expected {
		// Walk the struct and find the field with the yaml tag that matches the map key name.
		fieldIndex := -1
		for i := 0; i < fields; i++ {
			field := actualType.Field(i)
			name := yamlTagCleaner(field.Tag.Get("yaml"))
			if key == name {
				fieldIndex = i
				break
			}
		}
		if fieldIndex < 0 {
			t.Errorf("key is not mapped: %s->%s", parent, key)
		} else {
			// Now we can compare the values to recur into nested maps.
			actualValue := reflect.ValueOf(actual).Field(fieldIndex)

			switch expectedValue := value.(type) {
			case map[string]any:
				// Recurse to find more mismatches.
				structField := actualType.Field(fieldIndex)
				name := yamlTagCleaner(structField.Tag.Get("yaml"))
				compareMap(t, parent+"->"+name, expectedValue, actualValue.Interface())
			case []any:
				// Also check structs that are in arrays.
				for _, v := range expectedValue {
					if v, ok := v.(map[string]any); ok {
						structField := actualType.Field(fieldIndex)
						name := yamlTagCleaner(structField.Tag.Get("yaml"))
						compareMap(t, parent+"->"+name, v, actualValue.Interface())
					}
				}
			default:
				// Values not matching isn't really a huge concern, but we've come this far.
				compareValues(t, parent, key, expectedValue, actualValue)
			}
		}
	}
}

func compareValues(t *testing.T, parent, key string, expected any, actual reflect.Value) {
	if expected == nil && actual.IsNil() {
		return
	}
	if actual.Kind() == reflect.Pointer {
		actual = actual.Elem()
	}
	if !reflect.DeepEqual(expected, actual.Interface()) {
		t.Errorf("values are not equal: %s->%s expected %v got %v", parent, key, expected, actual.Interface())
	}
}

func yamlTagCleaner(tag string) string {
	return strings.ReplaceAll(tag, ",omitempty", "")
}

const exampleJob = `---
job:
  package-manager: npm_and_yarn
  source:
    provider: github
    repo: dependabot/dependabot-core
    directory: "/npm_and_yarn/helpers"
    api-endpoint: https://api.github.com/
    hostname: github.com
  dependencies:
  - got
  existing-pull-requests:
  - - dependency-name: npm
      dependency-version: 6.14.0
  - - dependency-name: prettier
      dependency-version: 2.0.1
  - - dependency-name: semver
      dependency-version: 7.2.1
  - - dependency-name: semver
      dependency-version: 7.2.2
  - - dependency-name: semver
      dependency-version: 7.3.0
  - - dependency-name: prettier
      dependency-version: 2.0.5
  - - dependency-name: jest
      dependency-version: 25.5.0
  - - dependency-name: jest
      dependency-version: 25.5.3
  - - dependency-name: jest
      dependency-version: 25.5.4
  - - dependency-name: jest
      dependency-version: 26.0.0
  - - dependency-name: npm
      dependency-version: 6.14.5
  - - dependency-name: jest
      dependency-version: 26.0.1
  - - dependency-name: eslint
      dependency-version: 7.0.0
  - - dependency-name: eslint
      dependency-version: 7.1.0
  - - dependency-name: npm
      dependency-version: 6.14.6
  - - dependency-name: lodash
      dependency-version: 4.17.19
  - - dependency-name: eslint
      dependency-version: 7.6.0
  - - dependency-name: npm
      dependency-version: 6.14.7
  - - dependency-name: jest
      dependency-version: 26.3.0
  - - dependency-name: jest
      dependency-version: 26.4.1
  - - dependency-name: prettier
      dependency-version: 2.1.0
  - - dependency-name: eslint
      dependency-version: 7.8.0
  - - dependency-name: eslint
      dependency-version: 7.13.0
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 1.0.11
  - - dependency-name: prettier
      dependency-version: 2.2.0
  - - dependency-name: eslint
      dependency-version: 7.19.0
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 2.2.5
  - - dependency-name: eslint
      dependency-version: 7.21.0
  - - dependency-name: npm
      dependency-version: 7.6.1
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 2.2.7
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 2.2.8
  - - dependency-name: semver
      dependency-version: 7.3.5
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 2.4.4
  - - dependency-name: detect-indent
      dependency-version: 6.1.0
  - - dependency-name: prettier
      dependency-version: 2.3.1
  - - dependency-name: jest
      dependency-version: 27.0.6
  - - dependency-name: detect-indent
      dependency-version: 7.0.0
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 2.8.1
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 2.8.2
  - - dependency-name: npm
      dependency-version: 6.14.15
  - - dependency-name: jest
      dependency-version: 27.1.0
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 2.8.3
  - - dependency-name: jest
      dependency-version: 27.1.1
  - - dependency-name: prettier
      dependency-version: 2.4.0
  - - dependency-name: jest
      dependency-version: 27.2.0
  - - dependency-name: jest
      dependency-version: 27.2.1
  - - dependency-name: jest
      dependency-version: 27.2.2
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 2.9.0
  - - dependency-name: jest
      dependency-version: 27.2.3
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 2.10.0
  - - dependency-name: npm
      dependency-version: 8.0.0
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 4.0.0
  - - dependency-name: eslint
      dependency-version: 8.0.1
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 4.0.1
  - - dependency-name: npm
      dependency-version: 8.1.0
  - - dependency-name: jest
      dependency-version: 27.3.0
  - - dependency-name: jest
      dependency-version: 27.3.1
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 4.0.2
  - - dependency-name: npm
      dependency-version: 8.1.1
  - - dependency-name: eslint
      dependency-version: 8.1.0
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 4.0.3
  - - dependency-name: npm
      dependency-version: 8.1.2
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 4.0.4
  - - dependency-name: npm
      dependency-version: 8.1.3
  - - dependency-name: eslint
      dependency-version: 8.2.0
  - - dependency-name: npm
      dependency-version: 8.1.4
  - - dependency-name: prettier
      dependency-version: 2.5.0
  - - dependency-name: jest
      dependency-version: 27.4.0
  - - dependency-name: jest
      dependency-version: 27.4.2
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 4.1.0
  - - dependency-name: eslint
      dependency-version: 8.4.0
  - - dependency-name: jest
      dependency-version: 27.4.4
  - - dependency-name: jest
      dependency-version: 27.4.6
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 4.1.2
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 4.2.0
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 4.3.0
  - - dependency-name: eslint
      dependency-version: 8.8.0
  - - dependency-name: jest
      dependency-version: 27.5.0
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 4.3.1
  - - dependency-name: eslint
      dependency-version: 8.9.0
  - - dependency-name: eslint-config-prettier
      dependency-version: 8.4.0
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 5.0.0
  - - dependency-name: eslint
      dependency-version: 8.10.0
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 5.0.1
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 5.0.2
  - - dependency-name: eslint
      dependency-version: 8.11.0
  - - dependency-name: prettier
      dependency-version: 2.6.1
  - - dependency-name: eslint
      dependency-version: 8.14.0
  - - dependency-name: jest
      dependency-version: 28.0.0
  - - dependency-name: jest
      dependency-version: 28.0.1
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 5.1.1
  - - dependency-name: jest
      dependency-version: 28.0.2
  - - dependency-name: jest
      dependency-version: 28.0.3
  - - dependency-name: eslint
      dependency-version: 8.16.0
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 5.2.1
  - - dependency-name: eslint
      dependency-version: 8.17.0
  - - dependency-name: prettier
      dependency-version: 2.7.0
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 5.2.2
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 5.3.0
  - - dependency-name: eslint
      dependency-version: 8.20.0
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 5.4.0
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 5.5.0
  - - dependency-name: jest
      dependency-version: 29.0.1
  - - dependency-name: eslint
      dependency-version: 8.23.0
  - - dependency-name: jest
      dependency-version: 29.0.2
  - - dependency-name: jest
      dependency-version: 29.0.3
  - - dependency-name: detect-indent
      dependency-version: 7.0.1
  - - dependency-name: got
      dependency-removed: true
    - dependency-name: npm
      dependency-version: 8.19.2
  - - dependency-name: "@npmcli/arborist"
      dependency-version: 5.6.2
  updating-a-pull-request: false
  lockfile-only: false
  update-subdependencies: false
  ignore-conditions:
  - dependency-name: npm
    version-requirement:
    update-types:
    - version-update:semver-major
    source: ".github/dependabot.yml"
  - dependency-name: npm
    version-requirement: ">= 7.a, < 8"
    update-types:
    source: "@dependabot ignore command"
  requirements-update-strategy:
  allowed-updates:
  - dependency-type: direct
    update-type: all
  - dependency-name: "rails"
    update-types:
    - "version-update:semver-minor"
    - "version-update:semver-patch"
  dependency-groups:
  - name: npm
    rules:
      patterns: ["npm", "@npmcli*"]
  security-advisories:
  - dependency-name: got
    patched-versions: []
    unaffected-versions: []
    affected-versions:
    - "< 11.8.5"
    - ">= 12.0.0 < 12.1.0"
  max-updater-run-time: 1800
  vendor-dependencies: false
  experiments:
    build-pull-request-message: true
    npm-transitive-dependency-removal: true
    npm-transitive-security-updates: true
  reject-external-code: false
  commit-message-options:
    prefix:
    prefix-development:
    include-scope:
  security-updates-only: true
  repo-private: false
  cooldown:
    default-days: 3
    semver-major-days: 7
    semver-minor-days: 5
    semver-patch-days: 2
    include:
      - dependency-name-1
      - dependency-name-2
    exclude:
      - dependency-name-3
      - dependency-name-4
  exclude-paths:
    - "docs/"
    - "examples/"
    - "test/"
  multi-ecosystem-update: false
`
