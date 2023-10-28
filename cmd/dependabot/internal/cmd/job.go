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
		if v := numPresent(jobId, jobToken, credentialsToken, dependabotAPIURL); v == 0 {
			// running in actions, pull data from environment
			params, err := github.Context()
			if err != nil {
				return err
			}
			if params == nil {
				return errors.New("no job parameters provided")
			}

			jobId = params.Payload.Inputs.JobID
			jobToken = params.Payload.Inputs.JobToken
			credentialsToken = params.Payload.Inputs.CredentialsToken
			dependabotAPIURL = params.Payload.Inputs.APIURL

			core.SetSecret(jobToken)
			core.SetSecret(credentialsToken)
		} else if v < 4 {
			return errors.New("must provide all of job-id, job-token, credentials-token, and api-url")
		}

		jobParameters := github.JobParameters{
			JobID:            jobId,
			JobToken:         jobToken,
			CredentialsToken: credentialsToken,
			APIURL:           dependabotAPIURL,
		}

		apiClient := client.New(dependabotAPIURL, &jobParameters)
		job, err := apiClient.JobDetails(context.Background())
		if err != nil {
			return err
		}
		credentials, err := apiClient.Credentials(context.Background())
		if err != nil {
			return err
		}

		if err := infra.Run(infra.RunParams{
			ProxyCertPath:    proxyCertPath,
			JobID:            jobId,
			JobToken:         jobToken,
			DependabotAPIURL: dependabotAPIURL,
			Job:              job,
			Creds:            credentials,
		}); err != nil {
			log.Fatal(err)
		}

		return nil
	},
}

func numPresent(args ...string) (count int) {
	for _, arg := range args {
		if arg != "" {
			count++
		}
	}
	return
}

var (
	jobId            string
	jobToken         string
	credentialsToken string
	dependabotAPIURL string
)

func init() {
	rootCmd.AddCommand(jobCmd)

	jobCmd.Flags().StringVarP(&jobId, "job-id", "j", "", "job id")
	jobCmd.Flags().StringVarP(&jobToken, "job-token", "t", "", "token used to fetch job details")
	jobCmd.Flags().StringVarP(&credentialsToken, "credentials-token", "c", "", "token used to fetch credentials")
	jobCmd.Flags().StringVarP(&dependabotAPIURL, "dependabot-api-url", "u", "", "URL that will be queried for job details and credentials")

	jobCmd.Flags().StringVar(&proxyCertPath, "proxy-cert", "", "path to a certificate the proxy will trust")
}
