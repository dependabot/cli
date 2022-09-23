package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/dependabot/cli/internal/model"
	"gopkg.in/yaml.v3"
)

// API intercepts calls to the Dependabot API
type API struct {
	// Expectations is the list of expectations that haven't been met yet
	Expectations []model.Output
	// Errors is the error list populated by doing a Dependabot run
	Errors []error
	// Actual will contain the scenario output that actually happened after the run is Complete
	Actual model.Scenario

	server          *http.Server
	cursor          int
	hasExpectations bool
	port            int
}

// NewAPI creates a new API instance and starts the server
func NewAPI(expected []model.Output) *API {
	fakeAPIHost := "127.0.0.1"
	if runtime.GOOS == "linux" {
		fakeAPIHost = "0.0.0.0"
	}
	if os.Getenv("FAKE_API_HOST") != "" {
		fakeAPIHost = os.Getenv("FAKE_API_HOST")
	}
	// Bind to port 0 for arbitrary port assignment
	l, err := net.Listen("tcp", fakeAPIHost+":0")
	if err != nil {
		panic(err)
	}
	server := &http.Server{
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	api := &API{
		server:          server,
		Expectations:    expected,
		cursor:          0,
		hasExpectations: len(expected) > 0,
		port:            l.Addr().(*net.TCPAddr).Port,
	}
	server.Handler = api

	go func() {
		if err := server.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	return api
}

// Port returns the port the API is listening on
func (a *API) Port() int {
	return a.port
}

// Stop stops the server
func (a *API) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	_ = a.server.Shutdown(ctx)
	cancel()
}

// Complete adds any remaining expectations to the error queue
func (a *API) Complete() {
	for i := a.cursor; i < len(a.Expectations); i++ {
		exp := &a.Expectations[i]
		a.Errors = append(a.Errors, fmt.Errorf("expectation not met: %v\n%v", exp.Type, exp.Expect))
	}
}

// ServeHTTP handles requests to the server
func (a *API) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		err = fmt.Errorf("failed to read body: %w", err)
		a.pushError(err)
		return
	}
	if err = r.Body.Close(); err != nil {
		err = fmt.Errorf("failed to close body: %w", err)
		a.pushError(err)
		return
	}

	parts := strings.Split(r.URL.String(), "/")
	kind := parts[len(parts)-1]

	if err := a.pushResult(kind, data); err != nil {
		a.pushError(err)
		return
	}

	if !a.hasExpectations {
		return
	}

	a.assertExpectation(kind, data)
}

func (a *API) assertExpectation(kind string, actualData []byte) {
	if len(a.Expectations) <= a.cursor {
		err := fmt.Errorf("missing expectation")
		a.pushError(err)
		return
	}
	expect := &a.Expectations[a.cursor]
	a.cursor++
	if kind != expect.Type {
		err := fmt.Errorf("type was unexpected: expected %v got %v", expect.Type, kind)
		a.pushError(err)
	}
	expectJSON, _ := json.Marshal(expect.Expect)
	// pretty both for sorting and can use simple comparison
	prettyData, _ := pretty(string(expectJSON))
	actual, _ := pretty(string(actualData))
	if actual != prettyData {
		err := fmt.Errorf("expected output doesn't match actual data received")
		a.pushError(err)

		// print diff to stdout
		dmp := diffmatchpatch.New()

		const checklines = false
		diffs := dmp.DiffMain(prettyData, actual, checklines)

		diffs = dmp.DiffCleanupSemantic(diffs)

		fmt.Println(dmp.DiffPrettyText(diffs))
	}
}

func (a *API) pushError(err error) {
	escapedError := strings.ReplaceAll(err.Error(), "\n", "")
	escapedError = strings.ReplaceAll(escapedError, "\r", "")
	log.Println(escapedError)
	a.Errors = append(a.Errors, err)
}

func (a *API) pushResult(kind string, data []byte) error {
	actual, err := decodeWrapper(kind, data)
	if err != nil {
		return err
	}
	// TODO validate required data
	output := model.Output{
		Type:   kind,
		Expect: *actual,
	}
	a.Actual.Output = append(a.Actual.Output, output)

	if msg, ok := actual.Data.(model.MarkAsProcessed); ok {
		// record the commit SHA so the test is reproducible
		a.Actual.Input.Job.Source.Commit = &msg.BaseCommitSha
	}

	return nil
}

// pretty indents and sorts the keys for a consistent comparison
func pretty(jsonString string) (string, error) {
	var v map[string]any
	if err := json.Unmarshal([]byte(jsonString), &v); err != nil {
		return "", err
	}
	removeNullsFromObjects(v)
	// shouldn't be possible to error
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b), nil
}

func removeNullsFromObjects(m map[string]any) {
	for k, v := range m {
		switch assertedVal := v.(type) {
		case nil:
			delete(m, k)
		case map[string]any:
			removeNullsFromObjects(assertedVal)
		case []any:
			for _, item := range assertedVal {
				switch assertedItem := item.(type) {
				case map[string]any:
					removeNullsFromObjects(assertedItem)
				}
			}
		}
	}
}

func decodeWrapper(kind string, data []byte) (*model.UpdateWrapper, error) {
	var actual model.UpdateWrapper
	switch kind {
	case "update_dependency_list":
		actual.Data = decode[model.UpdateDependencyList](data)
	case "create_pull_request":
		actual.Data = decode[model.CreatePullRequest](data)
	case "update_pull_request":
		actual.Data = decode[model.UpdatePullRequest](data)
	case "close_pull_request":
		actual.Data = decode[model.ClosePullRequest](data)
	case "mark_as_processed":
		actual.Data = decode[model.MarkAsProcessed](data)
	case "record_package_manager_version":
		actual.Data = decode[model.RecordPackageManagerVersion](data)
	case "record_update_job_error":
		actual.Data = decode[map[string]any](data)
	default:
		return nil, fmt.Errorf("unexpected output type: %s", kind)
	}
	return &actual, nil
}

func decode[T any](data []byte) any {
	var wrapper struct {
		Data T `json:"data" yaml:"data"`
	}
	err := yaml.NewDecoder(bytes.NewBuffer(data)).Decode(&wrapper)
	if err != nil {
		panic(err)
	}
	return wrapper.Data
}
