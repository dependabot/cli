package model

// SmokeTest is a way to test a job by asserting the outputs.
type SmokeTest struct {
	// Input is the input parameters
	Input Input `yaml:"input"`
	// Output is the list of expected outputs
	Output []Output `yaml:"output,omitempty"`
}

// Input is the input to a job
type Input struct {
	// Job is the data given to the updater
	Job Job `yaml:"job"`
	// Credentials is the registry info and tokens to pass to the Proxy
	Credentials []Credential `yaml:"credentials,omitempty"`
}

// Output is the expected output given the inputs
type Output struct {
	// Type is the kind of data to be checked, e.g. update_dependency_list, create_pull_request, etc
	Type string `yaml:"type"`
	// Expect is the data expected to be sent
	Expect UpdateWrapper `yaml:"expect"`
}
