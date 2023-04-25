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

var (
	jobs int
)

var testCmd = &cobra.Command{
	Use:   "test -f <scenario.yml>",
	Short: "Test scenarios",
	RunE: func(cmd *cobra.Command, args []string) error {
		if jobs < 1 {
			return fmt.Errorf("workers must be greater than or equal to 1")
		}

		if file == "" {
			return fmt.Errorf("requires a scenario file")
		}

		scenario, inputRaw, err := readScenarioFile(file)
		if err != nil {
			return err
		}

		processInput(&scenario.Input)

		if err := infra.Run(infra.RunParams{
			CacheDir:      cache,
			Creds:         scenario.Input.Credentials,
			Debug:         debugging,
			Expected:      scenario.Output,
			ExtraHosts:    extraHosts,
			InputName:     file,
			InputRaw:      inputRaw,
			Job:           &scenario.Input.Job,
			LocalDir:      local,
			Output:        output,
			ProxyCertPath: proxyCertPath,
			ProxyImage:    proxyImage,
			PullImages:    pullImages,
			Timeout:       timeout,
			UpdaterImage:  updaterImage,
			Volumes:       volumes,
		}); err != nil {
			log.Fatal(err)
		}

		return nil
	},
}

func readScenarioFile(file string) (*model.Scenario, []byte, error) {
	var scenario model.Scenario

	data, err := os.ReadFile(file)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open scenario file: %w", err)
	}
	if err = json.Unmarshal(data, &scenario); err != nil {
		if err = yaml.Unmarshal(data, &scenario); err != nil {
			return nil, nil, fmt.Errorf("failed to decode scenario file: %w", err)
		}
	}

	return &scenario, data, nil
}

func init() {
	rootCmd.AddCommand(testCmd)

	testCmd.Flags().StringVarP(&file, "file", "f", "", "path to scenario file")
	testCmd.Flags().IntVarP(&jobs, "jobs", "j", 1, "Number of jobs to run simultaneously")
	testCmd.MarkFlagsMutuallyExclusive("jobs", "file")

	testCmd.Flags().StringVarP(&output, "output", "o", "", "write scenario to file")
	testCmd.Flags().StringVar(&cache, "cache", "", "cache import/export directory")
	testCmd.Flags().StringVar(&local, "local", "", "local directory to use as fetched source")
	testCmd.Flags().StringVar(&proxyCertPath, "proxy-cert", "", "path to a certificate the proxy will trust")
	testCmd.Flags().BoolVar(&pullImages, "pull", true, "pull the image if it isn't present")
	testCmd.Flags().BoolVar(&debugging, "debug", false, "run an interactive shell inside the updater")
	testCmd.Flags().StringArrayVarP(&volumes, "volume", "v", nil, "mount volumes in Docker")
	testCmd.Flags().StringArrayVar(&extraHosts, "extra-hosts", nil, "Docker extra hosts setting on the proxy")
	testCmd.Flags().DurationVarP(&timeout, "timeout", "t", 0, "max time to run an update")
}
