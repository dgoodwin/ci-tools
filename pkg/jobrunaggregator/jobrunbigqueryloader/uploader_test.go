package jobrunbigqueryloader

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/openshift/ci-tools/pkg/jobrunaggregator/jobrunaggregatorapi"
	"github.com/openshift/ci-tools/pkg/jobrunaggregator/jobrunaggregatorlib"
	"github.com/openshift/ci-tools/pkg/jobrunaggregator/jobrunaggregatorlib/mock"
)

const (
	testJob      = "my-test-job"
	testJobRunID = 192934771
)

func TestJobRunBigQueryLoaderOptions_uploadTestSuites(t *testing.T) {

	ctrl := gomock.NewController(t)

	type fields struct {
		JobName string
		//WorkingDir string
		//PayloadTag string

		JobRunInserter BigQueryInserter
		//TestRunInserter BigQueryInserter
	}
	type args struct {
		ctx           context.Context
		gcsClientMock jobrunaggregatorlib.CIGCSClient
		//jobRun  jobrunaggregatorapi.JobRunInfo
		//prowJob *prowv1.ProwJob
		//suites  *junit.TestSuites
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
		{
			name: "Foo bar",
			args: args{
				ctx: context.Background(),
				gcsClientMock: func() jobrunaggregatorlib.CIGCSClient {
					gcsc := mock.NewMockCIGCSClient(ctrl)
					gcsc.EXPECT().ListJobRunNames(gomock.Any(), testJob, fmt.Sprint(testJobRunID+1)).
						DoAndReturn(
							func(_ context.Context, _ string, _ string) (chan string, chan error, error) {
								jobCh := make(chan string, 100)
								errorCh := make(chan error, 100)
								defer close(jobCh)
								defer close(errorCh)
								jobCh <- "foo"
								jobCh <- "bar"
								return jobCh, errorCh, nil
							},
						)
					gcsc.EXPECT().ReadJobRunFromGCS(gomock.Any(), "foo", "?").Return(
						jobrunaggregatorapi.JobRunInfo{}, nil)
					return gcsc
				}(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &jobLoaderOptions{
				jobName:                   testJob,
				numberOfConcurrentReaders: 5,

				//WorkingDir:      tt.fields.WorkingDir,
				//PayloadTag:      tt.fields.PayloadTag,
				jobRunInserter: tt.fields.JobRunInserter,
				//testRunInserter: tt.fields.TestRunInserter,
				getLastJobRunWithDataFn: func(ctx context.Context, jobName string) (*jobrunaggregatorapi.JobRunRow, error) {
					return &jobrunaggregatorapi.JobRunRow{
						Name:       fmt.Sprint(testJobRunID),
						JobName:    testJob,
						StartTime:  time.Now(),
						EndTime:    time.Now(),
						ReleaseTag: "foo",
						Cluster:    "bar",
						Status:     "IDon'tKNowYet",
					}, nil
				},
				gcsClient: tt.args.gcsClientMock,
			}
			if err := o.Run(tt.args.ctx /*, tt.args.jobRun, tt.args.prowJob, tt.args.suites*/); (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
