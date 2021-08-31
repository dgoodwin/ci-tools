package jobrunbigqueryloader

import (
	"context"
	"fmt"

	"github.com/openshift/ci-tools/pkg/jobrunaggregator/jobrunaggregatorlib"

	"cloud.google.com/go/bigquery"
	"github.com/openshift/ci-tools/pkg/jobrunaggregator/jobrunaggregatorapi"
	"github.com/openshift/ci-tools/pkg/junit"
	prowv1 "k8s.io/test-infra/prow/apis/prowjobs/v1"
)

type testRunUploader struct {
	testRunInserter jobrunaggregatorlib.BigQueryInserter
}

func newTestRunUploader(testRunInserter jobrunaggregatorlib.BigQueryInserter) uploader {
	return &testRunUploader{
		testRunInserter: testRunInserter,
	}
}

func (o *testRunUploader) uploadContent(ctx context.Context, jobRun jobrunaggregatorapi.JobRunInfo, prowJob *prowv1.ProwJob) error {
	fmt.Printf("  uploading junit test runs: %q/%q\n", jobRun.GetJobName(), jobRun.GetJobRunID())
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

func (o *testRunUploader) uploadTestSuite(ctx context.Context, jobRun jobrunaggregatorapi.JobRunInfo, prowJob *prowv1.ProwJob, parentSuites []string, suite *junit.TestSuite) error {
	currSuites := append(parentSuites, suite.Name)
	for _, testSuite := range suite.Children {
		if err := o.uploadTestSuite(ctx, jobRun, prowJob, currSuites, testSuite); err != nil {
			return err
		}
	}

	toInsert := []bigquery.ValueSaver{}
	for i := range suite.TestCases {
		testCase := suite.TestCases[i]
		if testCase.SkipMessage != nil {
			continue
		}
		toInsert = append(toInsert, newTestRunRow(jobRun, prowJob, currSuites, testCase))
	}
	if err := o.testRunInserter.Put(ctx, toInsert); err != nil {
		return err
	}

	return nil
}
