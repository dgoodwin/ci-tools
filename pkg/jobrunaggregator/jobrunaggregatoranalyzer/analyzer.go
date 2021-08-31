package jobrunaggregatoranalyzer

import (
	"context"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openshift/ci-tools/pkg/junit"

	"sigs.k8s.io/yaml"

	"k8s.io/apimachinery/pkg/util/clock"

	"github.com/openshift/ci-tools/pkg/jobrunaggregator/jobrunaggregatorlib"

	"github.com/openshift/ci-tools/pkg/jobrunaggregator/jobrunaggregatorapi"
)

// JobRunAggregatorAnalyzerOptions
// 1. reads a local cache of prowjob.json and junit files for a particular job.
// 2. finds jobruns for the the specified payload tag
// 3. reads all junit for the each jobrun
// 4. constructs a synthentic junit that includes every test and assigns pass/fail to each test
type JobRunAggregatorAnalyzerOptions struct {
	jobRunLocator      jobrunaggregatorlib.JobRunLocator
	passFailCalculator baseline

	jobName    string
	payloadTag string
	workingDir string

	// jobRunStartEstimate is the time that we think the job runs we're aggregating started.
	// it should be within an hour, plus or minus.
	jobRunStartEstimate time.Time
	clock               clock.Clock
	timeout             time.Duration
}

func (o *JobRunAggregatorAnalyzerOptions) getFinishedRelatedJobs(ctx context.Context) ([]jobrunaggregatorapi.JobRunInfo, error) {
	errorsInARow := 0
	for {
		jobsToAggregate, err := o.getFinishedRelatedJobsRightNow(ctx)
		if err == nil {
			return jobsToAggregate, nil
		}
		if err != nil {
			if errorsInARow > 20 {
				return nil, err
			}
			errorsInARow++
			fmt.Printf("error finding jobs to aggregate: %v", err)
		}

		fmt.Printf("   waiting and will attempt to find related jobs in a minute\n")
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(1 * time.Minute):
			continue
		}
	}

	return nil, fmt.Errorf("how on earth did we get here??")
}

func (o *JobRunAggregatorAnalyzerOptions) getFinishedRelatedJobsRightNow(ctx context.Context) ([]jobrunaggregatorapi.JobRunInfo, error) {
	jobsToAggregate, err := o.jobRunLocator.FindRelatedJobs(ctx)
	if err != nil {
		return nil, err
	}

	notCompletedJobs := []string{}
	for _, currJob := range jobsToAggregate {
		prowJob, err := currJob.GetProwJob(ctx)
		if err != nil {
			return nil, err
		}
		if prowJob.Status.CompletionTime == nil {
			notCompletedJobs = append(notCompletedJobs, currJob.GetJobRunID())
		}
	}

	if len(notCompletedJobs) > 0 {
		return nil, fmt.Errorf("%v are not completed", strings.Join(notCompletedJobs, ","))
	}
	return jobsToAggregate, nil
}

func (o *JobRunAggregatorAnalyzerOptions) Run(ctx context.Context) error {
	fmt.Printf("Aggregating job runs of type %q for %q.\n", o.jobName, o.payloadTag)
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	currentAggregationDir := filepath.Join(o.workingDir, o.jobName, o.payloadTag)
	if err := os.MkdirAll(currentAggregationDir, 0755); err != nil {
		return fmt.Errorf("error creating destination directory %q: %w", currentAggregationDir, err)
	}

	// if it hasn't been more than hour since the jobRuns started, the list isn't complete.
	// wait until we're ready
	now := o.clock.Now()
	readyAt := o.jobRunStartEstimate.Add(1 * time.Hour)
	if readyAt.After(now) {
		timeToWait := readyAt.Sub(now)
		fmt.Printf("Waiting for %v until %v.\n", timeToWait, readyAt)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(timeToWait):
		}
	}

	jobsToAggregate, err := o.getFinishedRelatedJobs(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%q for %q: found %d jobRuns.\n", o.jobName, o.payloadTag, len(jobsToAggregate))

	aggregationConfiguration := &AggregationConfiguration{}
	currentAggregationJunit := &aggregatedJobRunJunit{}
	for i := range jobsToAggregate {
		jobRun := jobsToAggregate[i]
		currJunit, err := newJobRunJunit(ctx, jobRun)
		if err != nil {
			return err
		}
		prowJob, err := currJunit.jobRun.GetProwJob(ctx)
		if err != nil {
			return err
		}
		aggregationConfiguration.IndividualJobs = append(
			aggregationConfiguration.IndividualJobs,
			JobRunInfo{
				JobName:      jobRun.GetJobName(),
				JobRunID:     jobRun.GetJobRunID(),
				HumanURL:     jobRun.GetHumanURL(),
				GCSBucketURL: jobRun.GetGCSArtifactURL(),
				Status:       string(prowJob.Status.State),
			},
		)

		currentAggregationJunit.addJobRun(jobrunaggregatorlib.GetPayloadTagFromProwJob(prowJob), currJunit)
	}

	fmt.Printf("%q for %q:  aggregating junit tests.\n", o.jobName, o.payloadTag)
	currentAggregationJunitSuites, err := currentAggregationJunit.aggregateAllJobRuns()
	if err != nil {
		return err
	}
	if err := assignPassFail(ctx, currentAggregationJunitSuites, o.passFailCalculator); err != nil {
		return err
	}
	currentAggrationJunitXML, err := xml.Marshal(currentAggregationJunitSuites)
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(currentAggregationDir, "junit-aggregated.xml"), currentAggrationJunitXML, 0644); err != nil {
		return err
	}

	aggregationConfigYAML, err := yaml.Marshal(aggregationConfiguration)
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(currentAggregationDir, "aggregation-config.yaml"), aggregationConfigYAML, 0644); err != nil {
		return err
	}

	fmt.Printf("%q for %q:  Done aggregating.\n", o.jobName, o.payloadTag)

	// now scan for a failure
	fakeSuite := &junit.TestSuite{Children: currentAggregationJunitSuites.Suites}
	outputTestCaseFailures([]string{"root"}, fakeSuite)

	return nil
}

func outputTestCaseFailures(parents []string, suite *junit.TestSuite) {
	currSuite := append(parents, suite.Name)
	for _, testCase := range suite.TestCases {
		if testCase.FailureOutput == nil {
			continue
		}
		if len(testCase.FailureOutput.Output) == 0 && len(testCase.FailureOutput.Message) == 0 {
			continue
		}
		fmt.Printf("Test Failed! suite=[%s], testCase=%v\nMessage: %v\n%v\n\n",
			strings.Join(currSuite, "  "),
			testCase.Name,
			testCase.FailureOutput.Message,
			testCase.SystemOut)
	}

	for _, child := range suite.Children {
		outputTestCaseFailures(currSuite, child)
	}
}
