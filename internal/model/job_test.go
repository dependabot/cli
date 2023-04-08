package model

import (
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
					switch v := v.(type) {
					case map[string]any:
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
    branch: 
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
  dependency-groups:
  - name: npm
    rules: ["npm", "@npmcli*"]
  credentials-metadata:
  - type: git_source
    host: github.com
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
`
