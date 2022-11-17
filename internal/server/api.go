package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kylelemons/godebug/diff"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"strings"
	"time"

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
	actual, err := decodeWrapper(kind, data)
	if err != nil {
		a.pushError(err)
	}

	if err := a.pushResult(kind, actual); err != nil {
		a.pushError(err)
		return
	}

	if !a.hasExpectations {
		return
	}

	a.assertExpectation(kind, actual)
}

func (a *API) assertExpectation(kind string, actual *model.UpdateWrapper) {
	if len(a.Expectations) <= a.cursor {
		err := fmt.Errorf("missing expectation")
		a.pushError(err)
		return
	}
	expect := &a.Expectations[a.cursor]
	a.cursor++
	if kind != expect.Type {
		err := fmt.Errorf("type has differences: expected %v got %v", expect.Type, kind)
		a.pushError(err)
		return
	}
	// need to use decodeWrapper to get the right type to match the actual type
	data, err := json.Marshal(expect.Expect)
	if err != nil {
		panic(err)
	}
	expected, err := decodeWrapper(expect.Type, data)
	if err != nil {
		panic(err)
	}
	if err = compare(expected, actual); err != nil {
		a.pushError(err)
	}
}

func (a *API) pushError(err error) {
	escapedError := strings.ReplaceAll(err.Error(), "\n", "")
	escapedError = strings.ReplaceAll(escapedError, "\r", "")
	log.Println(escapedError)
	a.Errors = append(a.Errors, err)
}

func (a *API) pushResult(kind string, actual *model.UpdateWrapper) error {
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

func decodeWrapper(kind string, data []byte) (actual *model.UpdateWrapper, err error) {
	actual = &model.UpdateWrapper{}
	switch kind {
	case "update_dependency_list":
		actual.Data, err = decode[model.UpdateDependencyList](data)
	case "create_pull_request":
		actual.Data, err = decode[model.CreatePullRequest](data)
	case "update_pull_request":
		actual.Data, err = decode[model.UpdatePullRequest](data)
	case "close_pull_request":
		actual.Data, err = decode[model.ClosePullRequest](data)
	case "mark_as_processed":
		actual.Data, err = decode[model.MarkAsProcessed](data)
	case "record_package_manager_version":
		actual.Data, err = decode[model.RecordPackageManagerVersion](data)
	case "record_update_job_error":
		actual.Data, err = decode[model.RecordUpdateJobError](data)
	default:
		return nil, fmt.Errorf("unexpected output type: %s", kind)
	}
	return actual, err
}

func decode[T any](data []byte) (any, error) {
	var wrapper struct {
		Data T `json:"data" yaml:"data"`
	}
	decoder := yaml.NewDecoder(bytes.NewBuffer(data))
	decoder.KnownFields(true)
	err := decoder.Decode(&wrapper)
	if err != nil {
		return nil, err
	}
	return wrapper.Data, nil
}

func compare(expect, actual *model.UpdateWrapper) error {
	switch v := expect.Data.(type) {
	case model.UpdateDependencyList:
		if !simpleCompare(v, actual.Data.(model.UpdateDependencyList)) {
			return fmt.Errorf("update_dependency_list has differences")
		}
	case model.CreatePullRequest:
		if !compareCreatePR(v, actual.Data.(model.CreatePullRequest)) {
			return fmt.Errorf("create_pull_request has differences")
		}
	case model.UpdatePullRequest:
		if !compareUpdatePR(v, actual.Data.(model.UpdatePullRequest)) {
			return fmt.Errorf("update_pull_request has differences")
		}
	case model.ClosePullRequest:
		if !simpleCompare(v, actual.Data.(model.ClosePullRequest)) {
			return fmt.Errorf("close_pull_request has differences")
		}
	case model.RecordPackageManagerVersion:
		if !simpleCompare(v, actual.Data.(model.RecordPackageManagerVersion)) {
			return fmt.Errorf("record_package_manager_version has differences")
		}
	case model.MarkAsProcessed:
		if !simpleCompare(v, actual.Data.(model.MarkAsProcessed)) {
			return fmt.Errorf("mark_as_processed has differences")
		}
	case model.RecordUpdateJobError:
		if !simpleCompare(v, actual.Data.(model.RecordUpdateJobError)) {
			return fmt.Errorf("record_update_job_error has differences")
		}
	default:
		return fmt.Errorf("unexpected type: %s", reflect.TypeOf(v))
	}

	return nil
}

func simpleCompare(expect, actual any) bool {
	if reflect.DeepEqual(expect, actual) {
		return true
	}
	actualBytes, err := yaml.Marshal(actual)
	if err != nil {
		panic(err)
	}
	expectBytes, err := yaml.Marshal(expect)
	if err != nil {
		panic(err)
	}
	fmt.Println(diff.Diff(string(expectBytes), string(actualBytes)))

	return false
}

const maxLines = 10

func compareCreatePR(expect, actual model.CreatePullRequest) bool {
	if reflect.DeepEqual(expect, actual) {
		return true
	}
	if expect.PRBody != actual.PRBody {
		if num := strings.Count(diff.Diff(expect.PRBody, actual.PRBody), "\n"); num > maxLines {
			fmt.Printf("pr-body has too many differences to display (%d)\n", num)
			expect.PRBody = ""
			actual.PRBody = ""
		}
	}
	for i := range expect.UpdatedDependencyFiles {
		expectedFile := &expect.UpdatedDependencyFiles[i]
		actualFile := &actual.UpdatedDependencyFiles[i]
		if num := strings.Count(diff.Diff(expectedFile.Content, actualFile.Content), "\n"); num > maxLines {
			fmt.Printf("file %s has too many differences to display (%d)\n", expectedFile.Name, num)
		}
	}
	expect.UpdatedDependencyFiles = nil
	actual.UpdatedDependencyFiles = nil

	actualBytes, err := yaml.Marshal(actual)
	if err != nil {
		panic(err)
	}
	expectBytes, err := yaml.Marshal(expect)
	if err != nil {
		panic(err)
	}

	fmt.Println(diff.Diff(string(expectBytes), string(actualBytes)))

	return false
}

func compareUpdatePR(expect, actual model.UpdatePullRequest) bool {
	if reflect.DeepEqual(expect, actual) {
		return true
	}

	if expect.PRBody != actual.PRBody {
		if num := strings.Count(diff.Diff(expect.PRBody, actual.PRBody), "\n"); num > maxLines {
			fmt.Printf("pr-body has too many differences to display (%d)\n", num)
			expect.PRBody = ""
			actual.PRBody = ""
		}
	}
	for i := range expect.UpdatedDependencyFiles {
		expectedFile := &expect.UpdatedDependencyFiles[i]
		actualFile := &actual.UpdatedDependencyFiles[i]
		if num := strings.Count(diff.Diff(expectedFile.Content, actualFile.Content), "\n"); num > maxLines {
			fmt.Printf("file %s has too many differences to display (%d)\n", expectedFile.Name, num)
		}
	}
	expect.UpdatedDependencyFiles = nil
	actual.UpdatedDependencyFiles = nil

	actualBytes, err := yaml.Marshal(actual)
	if err != nil {
		panic(err)
	}
	expectBytes, err := yaml.Marshal(expect)
	if err != nil {
		panic(err)
	}

	fmt.Println(diff.Diff(string(expectBytes), string(actualBytes)))

	return false
}
