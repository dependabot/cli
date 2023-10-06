package cmd

import (
	"context"
	"errors"
	"github.com/dependabot/cli/internal/actions/client"
	"github.com/dependabot/cli/internal/actions/core"
	"github.com/dependabot/cli/internal/actions/github"
	"github.com/dependabot/cli/internal/infra"
	"github.com/spf13/cobra"
	"log"
)

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Runs a Dependabot job in Actions",
	RunE: func(cmd *cobra.Command, args []string) error {
		params, err := github.Context()
		if err != nil {
			return err
		}
		if params == nil {
			return errors.New("no job parameters provided")
		}

		core.SetSecret(params.Payload.Inputs.JobToken)
		core.SetSecret(params.Payload.Inputs.CredentialsToken)

		apiClient := client.New(params.Payload.Inputs.DependabotAPIURL, &params.Payload.Inputs)
		job, err := apiClient.JobDetails(context.Background())
		if err != nil {
			return err
		}
		credentials, err := apiClient.Credentials(context.Background())
		if err != nil {
			return err
		}

		if err := infra.Run(infra.RunParams{
			Job:   job,
			Creds: credentials,
		}); err != nil {
			log.Fatal(err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(jobCmd)
}
