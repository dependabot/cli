package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/dependabot/cli/internal/infra"
	"github.com/dependabot/cli/internal/model"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// local variable for testing
var executeTestJob = infra.Run

func NewTestCommand() *cobra.Command {
	var flags SharedFlags

	cmd := &cobra.Command{
		Use:   "test -f <smoke-test.yml>",
		Short: "Run a smoke test",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.file == "" {
				return fmt.Errorf("requires a smoke test file to run, use -f <file>")
			}

			smokeTest, inputRaw, err := readSmokeTest(flags.file)
			if err != nil {
				return err
			}

			processInput(&smokeTest.Input, nil)

			if err := executeTestJob(infra.RunParams{
				CacheDir:            flags.cache,
				CollectorConfigPath: flags.collectorConfigPath,
				CollectorImage:      collectorImage,
				Creds:               smokeTest.Input.Credentials,
				Debug:               flags.debugging,
				Expected:            smokeTest.Output,
				ExtraHosts:          flags.extraHosts,
				InputName:           flags.file,
				InputRaw:            inputRaw,
				Job:                 &smokeTest.Input.Job,
				LocalDir:            flags.local,
				Output:              flags.output,
				ProxyCertPath:       flags.proxyCertPath,
				ProxyImage:          proxyImage,
				PullImages:          flags.pullImages,
				StorageImage:        storageImage,
				Timeout:             flags.timeout,
				UpdaterImage:        updaterImage,
				Volumes:             flags.volumes,
			}); err != nil {
				log.Fatal(err)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&flags.file, "file", "f", "", "path to the smoke test")

	cmd.Flags().StringVarP(&flags.output, "output", "o", "", "write a smoke test to file")
	cmd.Flags().StringVar(&flags.cache, "cache", "", "cache import/export directory")
	cmd.Flags().StringVar(&flags.local, "local", "", "local directory to use as fetched source")
	cmd.Flags().StringVar(&flags.proxyCertPath, "proxy-cert", "", "path to a certificate the proxy will trust")
	cmd.Flags().StringVar(&flags.collectorConfigPath, "collector-config", "", "path to an OpenTelemetry collector config file")
	cmd.Flags().BoolVar(&flags.pullImages, "pull", true, "pull the image if it isn't present")
	cmd.Flags().BoolVar(&flags.debugging, "debug", false, "run an interactive shell inside the updater")
	cmd.Flags().StringArrayVarP(&flags.volumes, "volume", "v", nil, "mount volumes in Docker")
	cmd.Flags().StringArrayVar(&flags.extraHosts, "extra-hosts", nil, "Docker extra hosts setting on the proxy")
	cmd.Flags().DurationVarP(&flags.timeout, "timeout", "t", 0, "max time to run an update")

	return cmd
}

var testCmd = NewTestCommand()

func readSmokeTest(file string) (*model.SmokeTest, []byte, error) {
	var smokeTest model.SmokeTest

	data, err := os.ReadFile(file)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open smoke test: %w", err)
	}
	if err = json.Unmarshal(data, &smokeTest); err != nil {
		if err = yaml.Unmarshal(data, &smokeTest); err != nil {
			return nil, nil, fmt.Errorf("failed to decode smoke test: %w", err)
		}
	}

	return &smokeTest, data, nil
}

func init() {
	rootCmd.AddCommand(testCmd)
}
