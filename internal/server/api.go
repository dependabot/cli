package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	writer          io.Writer
}

// NewAPI creates a new API instance and starts the server
func NewAPI(expected []model.Output, writer io.Writer) *API {
	fakeAPIHost := "127.0.0.1"
	if runtime.GOOS == "linux" {
		fakeAPIHost = "0.0.0.0"
	}
	if os.Getenv("FAKE_API_HOST") != "" {
		fakeAPIHost = os.Getenv("FAKE_API_HOST")
	}
	// Bind to port 0 for arbitrary port assignment
	port := "0"
	if os.Getenv("FAKE_API_PORT") != "" {
		port = os.Getenv("FAKE_API_PORT")
	}
	l, err := net.Listen("tcp", fakeAPIHost+":"+port)
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
		writer:          writer,
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

	if kind == "increment_metric" {
		// Let's just output the metrics data and stop
		a.outputRequestData(kind, actual)
		return
	}

	if err := a.pushResult(kind, actual); err != nil {
		a.pushError(err)
		return
	}

	if !a.hasExpectations {
		a.outputRequestData(kind, actual)
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
		err := fmt.Errorf("type was unexpected: expected %v got %v", expect.Type, kind)
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

func (a *API) outputRequestData(kind string, actual *model.UpdateWrapper) {
	if a.writer != nil {
		// output the data received to stdout
		if err := json.NewEncoder(a.writer).Encode(map[string]any{
			"type": kind,
			"data": actual.Data,
		}); err != nil {
			// Fail so the user knows stdout is not working
			log.Panicln("Failed to write to stdout: ", err)
		}
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
		a.Actual.Input.Job.Source.Commit = msg.BaseCommitSha
	}

	return nil
}

func decodeWrapper(kind string, data []byte) (actual *model.UpdateWrapper, err error) {
	actual = &model.UpdateWrapper{}
	switch kind {
	case "update_dependency_list":
		actual.Data, err = decode[model.UpdateDependencyList](data)
	case "create_pull_request":
		var createPR model.CreatePullRequest
		createPR, err = decode[model.CreatePullRequest](data)
		createPR.UpdatedDependencyFiles = replaceBinaryWithHash(createPR.UpdatedDependencyFiles)
		actual.Data = createPR
	case "update_pull_request":
		var updatePR model.UpdatePullRequest
		updatePR, err = decode[model.UpdatePullRequest](data)
		updatePR.UpdatedDependencyFiles = replaceBinaryWithHash(updatePR.UpdatedDependencyFiles)
		actual.Data = updatePR
	case "close_pull_request":
		actual.Data, err = decode[model.ClosePullRequest](data)
	case "mark_as_processed":
		actual.Data, err = decode[model.MarkAsProcessed](data)
	case "record_ecosystem_versions":
		actual.Data, err = decode[model.RecordEcosystemVersions](data)
	case "record_update_job_error":
		actual.Data, err = decode[model.RecordUpdateJobError](data)
	case "increment_metric":
		actual.Data, err = decode[model.IncrementMetric](data)
	default:
		return nil, fmt.Errorf("unexpected output type: %s", kind)
	}
	return actual, err
}

// to avoid having massive base64 encoded strings in the test fixtures, replace the content with a hash
func replaceBinaryWithHash(files []model.DependencyFile) []model.DependencyFile {
	for i := range files {
		file := &files[i]
		if file.ContentEncoding == "base64" {
			// since this is also called for the expected value, this needs to not be base64
			// otherwise it will calculate the checksum of the checksum
			file.ContentEncoding = "sha256"
			hash := sha256.Sum256([]byte(file.Content))
			file.Content = hex.EncodeToString(hash[:])
		}
	}
	return files
}

func decode[T any](data []byte) (T, error) {
	var wrapper struct {
		Data T `json:"data" yaml:"data"`
	}
	decoder := yaml.NewDecoder(bytes.NewBuffer(data))
	decoder.KnownFields(true)
	err := decoder.Decode(&wrapper)
	if err != nil {
		return *new(T), err
	}
	return wrapper.Data, nil
}

func compare(expect, actual *model.UpdateWrapper) error {
	switch v := expect.Data.(type) {
	case model.UpdateDependencyList:
		return compareUpdateDependencyList(v, actual.Data.(model.UpdateDependencyList))
	case model.CreatePullRequest:
		return compareCreatePullRequest(v, actual.Data.(model.CreatePullRequest))
	case model.UpdatePullRequest:
		return compareUpdatePullRequest(v, actual.Data.(model.UpdatePullRequest))
	case model.ClosePullRequest:
		return compareClosePullRequest(v, actual.Data.(model.ClosePullRequest))
	case model.RecordEcosystemVersions:
		return compareRecordEcosystemVersions(v, actual.Data.(model.RecordEcosystemVersions))
	case model.MarkAsProcessed:
		return compareMarkAsProcessed(v, actual.Data.(model.MarkAsProcessed))
	case model.RecordUpdateJobError:
		return compareRecordUpdateJobError(v, actual.Data.(model.RecordUpdateJobError))
	default:
		return fmt.Errorf("unexpected type: %s", reflect.TypeOf(v))
	}
}

func unexpectedBody(kind string) error {
	return fmt.Errorf("unexpected body for %s", kind)
}

func compareUpdateDependencyList(expect, actual model.UpdateDependencyList) error {
	if reflect.DeepEqual(expect, actual) {
		return nil
	}
	return unexpectedBody("update_dependency_list")
}

func compareCreatePullRequest(expect, actual model.CreatePullRequest) error {
	if reflect.DeepEqual(expect, actual) {
		return nil
	}
	return unexpectedBody("create_pull_request")
}

func compareUpdatePullRequest(expect, actual model.UpdatePullRequest) error {
	if reflect.DeepEqual(expect, actual) {
		return nil
	}
	return unexpectedBody("update_pull_request")
}

func compareClosePullRequest(expect, actual model.ClosePullRequest) error {
	if reflect.DeepEqual(expect, actual) {
		return nil
	}
	return unexpectedBody("close_pull_request")
}

func compareRecordEcosystemVersions(expect, actual model.RecordEcosystemVersions) error {
	if reflect.DeepEqual(expect, actual) {
		return nil
	}
	return unexpectedBody("record_ecosystem_versions")
}

func compareMarkAsProcessed(expect, actual model.MarkAsProcessed) error {
	if reflect.DeepEqual(expect, actual) {
		return nil
	}
	return unexpectedBody("mark_as_processed")
}

func compareRecordUpdateJobError(expect, actual model.RecordUpdateJobError) error {
	if reflect.DeepEqual(expect, actual) {
		return nil
	}
	return unexpectedBody("record_update_job_error")
}
