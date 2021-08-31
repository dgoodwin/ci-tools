package jobtableprimer

import (
	"context"

	"github.com/openshift/ci-tools/pkg/jobrunaggregator/jobrunaggregatorapi"

	"github.com/openshift/ci-tools/pkg/jobrunaggregator/jobrunaggregatorlib"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type primeJobTableFlags struct {
	DataCoordinates *jobrunaggregatorlib.BigQueryDataCoordinates
	Authentication  *jobrunaggregatorlib.GoogleAuthenticationFlags
}

func newPrimeJobTableFlags() *primeJobTableFlags {
	return &primeJobTableFlags{
		DataCoordinates: jobrunaggregatorlib.NewBigQueryDataCoordinates(),
		Authentication:  jobrunaggregatorlib.NewGoogleAuthenticationFlags(),
	}
}

func (f *primeJobTableFlags) BindFlags(fs *pflag.FlagSet) {
	f.DataCoordinates.BindFlags(fs)
	f.Authentication.BindFlags(fs)
}

func NewPrimeJobTableCommand() *cobra.Command {
	f := newPrimeJobTableFlags()

	cmd := &cobra.Command{
		Use:          "prime-job-table",
		Long:         `insert data into job table`,
		SilenceUsage: true,

		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			if err := f.Validate(); err != nil {
				logrus.WithError(err).Fatal("Flags are invalid")
			}
			o, err := f.ToOptions(ctx)
			if err != nil {
				logrus.WithError(err).Fatal("Failed to build runtime options")
			}

			if err := o.Run(ctx); err != nil {
				logrus.WithError(err).Fatal("Command failed")
			}

			return nil
		},

		Args: jobrunaggregatorlib.NoArgs,
	}

	f.BindFlags(cmd.Flags())

	return cmd
}

// Validate checks to see if the user-input is likely to produce functional runtime options
func (f *primeJobTableFlags) Validate() error {
	if err := f.DataCoordinates.Validate(); err != nil {
		return err
	}
	if err := f.Authentication.Validate(); err != nil {
		return err
	}

	return nil
}

// ToOptions goes from the user input to the runtime values need to run the command.
// Expect to see unit tests on the options, but not on the flags which are simply value mappings.
func (f *primeJobTableFlags) ToOptions(ctx context.Context) (*CreateJobsOptions, error) {
	client, err := f.Authentication.NewBigQueryClient(ctx, f.DataCoordinates.ProjectID)
	if err != nil {
		return nil, err
	}
	ciDataSet := client.Dataset(f.DataCoordinates.DataSetID)
	jobTable := ciDataSet.Table(jobrunaggregatorapi.JobsTableName)

	return &CreateJobsOptions{
		jobsToCreate: jobsToAnalyze,
		ciDataClient: jobrunaggregatorlib.NewCIDataClient(*f.DataCoordinates, client),

		jobInserter: jobTable.Inserter(),
	}, nil
}
