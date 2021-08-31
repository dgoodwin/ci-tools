package jobtableprimer

import (
	"context"
	"fmt"

	"github.com/openshift/ci-tools/pkg/jobrunaggregator/jobrunaggregatorapi"

	"github.com/openshift/ci-tools/pkg/jobrunaggregator/jobrunaggregatorlib"
)

type CreateJobsOptions struct {
	ciDataClient jobrunaggregatorlib.CIDataClient
	jobsToCreate []jobrunaggregatorapi.JobRow

	jobInserter jobrunaggregatorlib.BigQueryInserter
}

func (o *CreateJobsOptions) Run(ctx context.Context) error {
	fmt.Printf("Priming jobs\n")

	existingJobs, err := o.ciDataClient.ListAllJobs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get jobs: %w", err)
	}

	missingJobs := []jobrunaggregatorapi.JobRow{}
	for i := range o.jobsToCreate {
		jobToCreate := o.jobsToCreate[i]
		alreadyExists := false
		for _, existing := range existingJobs {
			if existing.JobName == jobToCreate.JobName {
				alreadyExists = true
				break
			}
		}
		if alreadyExists {
			continue
		}

		missingJobs = append(missingJobs, jobToCreate)
	}

	fmt.Printf("Inserting %d jobs\n", len(missingJobs))
	if err := o.jobInserter.Put(ctx, missingJobs); err != nil {
		return err
	}

	return nil
}
