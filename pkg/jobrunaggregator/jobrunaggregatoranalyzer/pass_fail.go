package jobrunaggregatoranalyzer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/openshift/ci-tools/pkg/jobrunaggregator/jobrunaggregatorapi"

	"github.com/openshift/ci-tools/pkg/jobrunaggregator/jobrunaggregatorlib"

	"github.com/openshift/ci-tools/pkg/junit"
	"gopkg.in/yaml.v2"
)

type baseline interface {
	FailureMessage(ctx context.Context, suiteNames []string, testCaseDetails *TestCaseDetails) (string, error)
}

func assignPassFail(ctx context.Context, combined *junit.TestSuites, baselinePassFail baseline) error {
	for _, currTestSuite := range combined.Suites {
		if err := assignPassFailForTestSuite(ctx, []string{}, currTestSuite, baselinePassFail); err != nil {
			return err
		}
	}

	return nil
}

func assignPassFailForTestSuite(ctx context.Context, parentTestSuites []string, combined *junit.TestSuite, baselinePassFail baseline) error {
	failureCount := uint(0)

	currSuiteNames := append(parentTestSuites, combined.Name)
	for _, currTestSuite := range combined.Children {
		if err := assignPassFailForTestSuite(ctx, currSuiteNames, currTestSuite, baselinePassFail); err != nil {
			return err
		}
		failureCount += currTestSuite.NumFailed
	}

	for _, currTestCase := range combined.TestCases {
		currDetails := &TestCaseDetails{}
		if err := yaml.Unmarshal([]byte(currTestCase.SystemOut), currDetails); err != nil {
			return err
		}
		failureMessage, err := baselinePassFail.FailureMessage(ctx, currSuiteNames, currDetails)
		if err != nil {
			return err
		}
		if len(failureMessage) == 0 {
			continue
		}
		currTestCase.FailureOutput = &junit.FailureOutput{
			Message: failureMessage,
			Output:  currTestCase.SystemOut,
		}
		failureCount++
	}

	combined.NumFailed = failureCount

	return nil
}

type simpleBaseline struct{}

func (*simpleBaseline) FailureMessage(ctx context.Context, suiteNames []string, testCaseDetails *TestCaseDetails) (string, error) {
	if !didTestRun(testCaseDetails) {
		return "", nil
	}

	if len(testCaseDetails.Passes) < 2 {
		return fmt.Sprintf("Passed %d times, failed %d times, skipped %d times: we require at least two passes",
			len(testCaseDetails.Passes),
			len(testCaseDetails.Failures),
			len(testCaseDetails.Skips),
		), nil
	}

	if passPercentage := getWorkingPercentage(testCaseDetails); passPercentage < 60 {
		return fmt.Sprintf("Pass percentage is %d\nPassed %d times, failed %d times, skipped %d times: we require at least 60%% passing",
			int(passPercentage),
			len(testCaseDetails.Passes),
			len(testCaseDetails.Failures),
			len(testCaseDetails.Skips),
		), nil
	}

	return "", nil
}

// weeklyAverageFromTenDays gets the weekly average pass rate from ten days ago so that the latest tests do not
// influence the pass/fail criteria.
type weeklyAverageFromTenDays struct {
	jobName                 string
	startDay                time.Time
	minimumNumberOfAttempts int
	bigQueryClient          jobrunaggregatorlib.CIDataClient

	queryTestRunsOnce        sync.Once
	queryTestRunsErr         error
	aggregatedTestRunsByName map[string]jobrunaggregatorapi.AggregatedTestRunRow
}

func newWeeklyAverageFromTenDaysAgo(jobName string, startDay time.Time, minimumNumberOfAttempts int, bigQueryClient jobrunaggregatorlib.CIDataClient) baseline {
	tenDayAgo := jobrunaggregatorlib.GetUTCDay(startDay).Add(-10 * 24 * time.Hour)

	return &weeklyAverageFromTenDays{
		jobName:                  jobName,
		startDay:                 tenDayAgo,
		minimumNumberOfAttempts:  minimumNumberOfAttempts,
		bigQueryClient:           bigQueryClient,
		queryTestRunsOnce:        sync.Once{},
		queryTestRunsErr:         nil,
		aggregatedTestRunsByName: nil,
	}
}

func (a *weeklyAverageFromTenDays) getAggregatedTestRuns(ctx context.Context) (map[string]jobrunaggregatorapi.AggregatedTestRunRow, error) {
	a.queryTestRunsOnce.Do(func() {
		rows, err := a.bigQueryClient.ListAggregatedTestRunsForJob(ctx, "ByOneWeek", a.jobName, a.startDay)
		a.aggregatedTestRunsByName = map[string]jobrunaggregatorapi.AggregatedTestRunRow{}
		if err != nil {
			a.queryTestRunsErr = err
			return
		}
		for i := range rows {
			row := rows[i]
			a.aggregatedTestRunsByName[row.TestName] = row
		}
	})

	return a.aggregatedTestRunsByName, a.queryTestRunsErr
}

func (a *weeklyAverageFromTenDays) FailureMessage(ctx context.Context, suiteNames []string, testCaseDetails *TestCaseDetails) (string, error) {
	if !didTestRun(testCaseDetails) {
		return "", nil
	}

	numberOfAttempts := getAttempts(testCaseDetails)
	numberOfPasses := getNumberOfPasses(testCaseDetails)
	numberOfFailures := getNumberOfFailures(testCaseDetails)
	if numberOfAttempts < a.minimumNumberOfAttempts {
		return fmt.Sprintf("Passed %d times, failed %d times, skipped %d times: we require at least five attempts to have a chance at success",
			numberOfPasses,
			numberOfFailures,
			len(testCaseDetails.Skips),
		), nil
	}
	if len(testCaseDetails.Passes) < 1 {
		return fmt.Sprintf("Passed %d times, failed %d times, skipped %d times: we require at least one pass to consider it a success",
			numberOfPasses,
			numberOfFailures,
			len(testCaseDetails.Skips),
		), nil
	}

	aggregatedTestRunsByName, err := a.getAggregatedTestRuns(ctx)
	if err != nil {
		return "", err
	}

	averageTestResult, ok := aggregatedTestRunsByName[testCaseDetails.Name]
	if !ok {
		return fmt.Sprintf("missing historical data for %v", testCaseDetails.Name), nil
	}

	requiredNumberOfPasses := requiredPassesByPassPercentageByNumberOfAttempts[numberOfAttempts][averageTestResult.WorkingPercentage]
	if numberOfPasses < requiredNumberOfPasses {
		return fmt.Sprintf("Passed %d times, failed %d times.  The historical pass rate is %d.  The required number of passes is %d.",
			numberOfPasses,
			numberOfFailures,
			averageTestResult.WorkingPercentage,
			requiredNumberOfPasses,
		), nil

	}

	return "", nil
}

var (
	requiredPassesByPassPercentageFor_10_Attempts = []int{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 00-09
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 10-19
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 20-29
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 30-39
		0, 1, 1, 1, 1, 1, 1, 1, 1, 1, // 40-49
		1, 1, 2, 2, 2, 2, 2, 2, 2, 2, // 50-59
		2, 2, 3, 3, 3, 3, 3, 3, 3, 3, // 60-69
		3, 4, 4, 4, 4, 4, 4, 4, 4, 5, // 70-79
		5, 5, 5, 5, 5, 5, 6, 6, 6, 6, // 80-89
		6, 6, 7, 7, 7, 7, 7, 7, 8, 8, // 90-99
		9, // 100
	}
	requiredPassesByPassPercentageFor_09_Attempts = []int{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 00-09
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 10-19
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 20-29
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 30-39
		0, 0, 0, 0, 1, 1, 1, 1, 1, 1, // 40-49
		1, 1, 1, 1, 1, 1, 2, 2, 2, 2, // 50-59
		2, 2, 2, 2, 2, 2, 2, 3, 3, 3, // 60-69
		3, 3, 3, 3, 3, 3, 5, 5, 5, 5, // 70-79
		5, 5, 5, 5, 6, 6, 6, 6, 6, 6, // 80-89
		6, 7, 7, 7, 7, 7, 7, 8, 8, 8, // 90-99
		9, // 100
	}
	requiredPassesByPassPercentageFor_08_Attempts = []int{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 00-09
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 10-19
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 20-29
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 30-39
		0, 0, 0, 0, 0, 0, 0, 0, 1, 1, // 40-49
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1, // 50-59
		1, 2, 2, 2, 2, 2, 2, 2, 2, 2, // 60-69
		2, 2, 3, 3, 3, 3, 3, 3, 3, 3, // 70-79
		3, 3, 4, 4, 4, 4, 4, 4, 4, 4, // 80-89
		5, 5, 5, 5, 5, 5, 7, 7, 7, 7, // 90-99
		8, // 100
	}
	requiredPassesByPassPercentageFor_07_Attempts = []int{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 00-09
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 10-19
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 20-29
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 30-39
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 40-49
		0, 0, 0, 2, 2, 2, 2, 2, 2, 2, // 50-59
		2, 2, 2, 2, 2, 2, 2, 3, 3, 3, // 60-69
		3, 3, 3, 3, 3, 3, 3, 3, 3, 4, // 70-79
		4, 4, 4, 4, 4, 4, 4, 4, 6, 6, // 80-89
		6, 6, 6, 6, 6, 7, 7, 7, 7, 7, // 90-99
		7, // 100
	}
	requiredPassesByPassPercentageFor_06_Attempts = []int{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 00-09
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 10-19
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 20-29
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 30-39
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 40-49
		0, 0, 0, 0, 0, 0, 0, 0, 0, 2, // 50-59
		2, 2, 2, 2, 2, 2, 2, 2, 2, 2, // 60-69
		2, 2, 2, 2, 4, 4, 4, 4, 4, 4, // 70-79
		4, 4, 4, 4, 4, 4, 5, 5, 5, 5, // 80-89
		5, 5, 5, 5, 5, 6, 6, 6, 6, 6, // 90-99
		6, // 100
	}
	requiredPassesByPassPercentageFor_05_Attempts = []int{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 00-09
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 10-19
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 20-29
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 30-39
		0, 0, 0, 0, 0, 0, 1, 1, 1, 1, // 40-49
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1, // 50-59
		1, 1, 1, 1, 1, 1, 1, 2, 2, 2, // 60-69
		2, 2, 2, 2, 2, 2, 2, 2, 2, 2, // 70-79
		2, 2, 5, 5, 5, 5, 5, 5, 5, 5, // 80-89
		5, 5, 5, 5, 5, 5, 5, 5, 5, 5, // 90-99
		5, // 100
	}
	requiredPassesByPassPercentageFor_04_Attempts = []int{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 00-09
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 10-19
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 20-29
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 30-39
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 40-49
		0, 0, 0, 0, 1, 1, 1, 1, 1, 1, // 50-59
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1, // 60-69
		1, 1, 1, 1, 1, 1, 3, 3, 3, 3, // 70-79
		3, 3, 3, 3, 3, 3, 3, 3, 3, 3, // 80-89
		3, 4, 4, 4, 4, 4, 4, 4, 4, 4, // 90-99
		4, // 100
	}
	requiredPassesByPassPercentageFor_03_Attempts = []int{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 00-09
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 10-19
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 20-29
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 30-39
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 40-49
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 50-59
		0, 0, 0, 0, 1, 1, 1, 1, 1, 1, // 60-69
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1, // 70-79
		1, 1, 1, 1, 1, 1, 1, 3, 3, 3, // 80-89
		3, 3, 3, 3, 3, 3, 3, 3, 3, 3, // 90-99
		3, // 100
	}
	requiredPassesByPassPercentageFor_02_Attempts = []int{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 00-09
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 10-19
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 20-29
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 30-39
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 40-49
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 50-59
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 60-69
		0, 0, 0, 0, 0, 0, 0, 0, 0, 2, // 70-79
		2, 2, 2, 2, 2, 2, 2, 2, 2, 2, // 80-89
		2, 2, 2, 2, 2, 2, 2, 2, 2, 2, // 90-99
		2, // 100
	}
	requiredPassesByPassPercentageFor_01_Attempts = []int{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 00-09
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 10-19
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 20-29
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 30-39
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 40-49
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 50-59
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 60-69
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 70-79
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 80-89
		0, 0, 0, 0, 0, 0, 1, 1, 1, 1, // 90-99
		1, // 100
	}
	requiredPassesByPassPercentageFor_00_Attempts = []int{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 00-09
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 10-19
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 20-29
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 30-39
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 40-49
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 50-59
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 60-69
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 70-79
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 80-89
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 90-99
		0, // 100
	}

	requiredPassesByPassPercentageByNumberOfAttempts = [][]int{
		requiredPassesByPassPercentageFor_00_Attempts,
		requiredPassesByPassPercentageFor_01_Attempts,
		requiredPassesByPassPercentageFor_02_Attempts,
		requiredPassesByPassPercentageFor_03_Attempts,
		requiredPassesByPassPercentageFor_04_Attempts,
		requiredPassesByPassPercentageFor_05_Attempts,
		requiredPassesByPassPercentageFor_06_Attempts,
		requiredPassesByPassPercentageFor_07_Attempts,
		requiredPassesByPassPercentageFor_08_Attempts,
		requiredPassesByPassPercentageFor_09_Attempts,
		requiredPassesByPassPercentageFor_10_Attempts,
	}
)

func didTestRun(testCaseDetails *TestCaseDetails) bool {
	if len(testCaseDetails.Passes) == 0 && len(testCaseDetails.Failures) == 0 {
		return false
	}
	return true
}

func getWorkingPercentage(testCaseDetails *TestCaseDetails) float32 {
	// if the same job run has a pass and a fail, then it's a flake.  For now, we will consider those as passes by subtracting
	// them (for now), from the failure count.
	failureCount := len(testCaseDetails.Failures)
	for _, failure := range testCaseDetails.Failures {
		for _, pass := range testCaseDetails.Passes {
			if pass.JobRunID == failure.JobRunID {
				failureCount--
				break
			}
		}
	}

	return float32(len(testCaseDetails.Passes)) / float32(len(testCaseDetails.Passes)+failureCount) * 100.0
}

func getAttempts(testCaseDetails *TestCaseDetails) int {
	// if the same job run has a pass and a fail, or multiple passes, it only counts as a single attempt.
	jobRunsThatAttempted := sets.NewString()
	for _, failure := range testCaseDetails.Failures {
		jobRunsThatAttempted.Insert(failure.JobRunID)
	}
	for _, pass := range testCaseDetails.Passes {
		jobRunsThatAttempted.Insert(pass.JobRunID)
	}

	return len(jobRunsThatAttempted)
}

func getNumberOfPasses(testCaseDetails *TestCaseDetails) int {
	// if the same job run has multiple passes, it only counts as a single pass.
	jobRunsThatPassed := sets.NewString()
	for _, pass := range testCaseDetails.Passes {
		jobRunsThatPassed.Insert(pass.JobRunID)
	}

	return len(jobRunsThatPassed)
}

func getNumberOfFailures(testCaseDetails *TestCaseDetails) int {
	// if the same job run has multiple failures, it only counts as a single failure.
	jobRunsThatFailed := sets.NewString()
	for _, failure := range testCaseDetails.Failures {
		jobRunsThatFailed.Insert(failure.JobRunID)
	}

	return len(jobRunsThatFailed)
}
