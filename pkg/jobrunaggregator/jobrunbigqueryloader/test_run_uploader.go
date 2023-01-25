package jobrunbigqueryloader

import (
	"context"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	prowv1 "k8s.io/test-infra/prow/apis/prowjobs/v1"

	"github.com/openshift/ci-tools/pkg/jobrunaggregator/jobrunaggregatorapi"
	"github.com/openshift/ci-tools/pkg/jobrunaggregator/jobrunaggregatorlib"
	"github.com/openshift/ci-tools/pkg/junit"
)

type testRunUploader struct {
	testRunInserter jobrunaggregatorlib.BigQueryInserter
	ciDataClient    jobrunaggregatorlib.CIDataClient
}

func newTestRunUploader(testRunInserter jobrunaggregatorlib.BigQueryInserter,
	ciDataClient jobrunaggregatorlib.CIDataClient) uploader {
	return &testRunUploader{
		testRunInserter: testRunInserter,
		ciDataClient:    ciDataClient,
	}
}

func (o *testRunUploader) getLastUploadedJobRunForJob(ctx context.Context, jobName string) (*jobrunaggregatorapi.JobRunRow, error) {
	return o.ciDataClient.GetLastJobRunFromTableForJobName(ctx, jobrunaggregatorapi.LegacyJobRunTableName, jobName)
}

func (o *testRunUploader) getLastUploadedJobRunEndTime(ctx context.Context) (*time.Time, error) {
	return o.ciDataClient.GetLastJobRunEndTimeFromTable(ctx, jobrunaggregatorapi.LegacyJobRunTableName)
}

func (o *testRunUploader) listUploadedJobRunIDsSince(ctx context.Context, since *time.Time) ([]string, error) {
	return o.ciDataClient.ListUploadedJobRunIDsSinceFromTable(ctx, jobrunaggregatorapi.LegacyJobRunTableName, since)
}

func (o *testRunUploader) uploadContent(ctx context.Context, jobRun jobrunaggregatorapi.JobRunInfo, prowJob *prowv1.ProwJob, logger logrus.FieldLogger) error {
	logger.Info("uploading junit test runs")
	combinedJunitContent, err := jobRun.GetCombinedJUnitTestSuites(ctx)
	if err != nil {
		return err
	}

	return o.uploadTestSuites(ctx, jobRun, prowJob, combinedJunitContent)
}

func (o *testRunUploader) uploadTestSuites(ctx context.Context, jobRun jobrunaggregatorapi.JobRunInfo, prowJob *prowv1.ProwJob, suites *junit.TestSuites) error {

	for _, testSuite := range suites.Suites {
		if err := o.uploadTestSuite(ctx, jobRun, prowJob, []string{}, testSuite); err != nil {
			return err
		}
	}
	return nil
}

func (o *testRunUploader) uploadTestSuite(ctx context.Context, jobRun jobrunaggregatorapi.JobRunInfo, prowJob *prowv1.ProwJob, parentSuites []string, suite *junit.TestSuite) error { //nolint
	currSuites := append(parentSuites, suite.Name)
	for _, testSuite := range suite.Children {
		if err := o.uploadTestSuite(ctx, jobRun, prowJob, currSuites, testSuite); err != nil {
			return err
		}
	}

	toInsert := []*jobrunaggregatorapi.TestRunRow{}
	for i := range suite.TestCases {
		testCase := suite.TestCases[i]
		if testCase.SkipMessage != nil {
			continue
		}

		var status string
		switch {
		case testCase.FailureOutput != nil:
			status = "Failed"
		case testCase.SkipMessage != nil:
			status = "Skipped"
		default:
			status = "Passed"
		}

		testSuiteStr := strings.Join(currSuites, jobrunaggregatorlib.TestSuitesSeparator)
		toInsert = append(toInsert, newTestRunRow(jobRun, status, testSuiteStr, testCase))
	}
	if err := o.testRunInserter.Put(ctx, toInsert); err != nil {
		return err
	}

	return nil
}
