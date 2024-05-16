package jobrunaggregatorapi

import (
	"cloud.google.com/go/bigquery"
)

const (
	unifiedBackendDisruptionSchema = `
SELECT 
  BackendDisruption.BackendName as BackendName,
  JobRuns.name as JobRunName,
  Jobs.Jobname as JobName,
  BackendDisruption.DisruptionSeconds as DisruptionSeconds,
  JobRuns.StartTime as JobRunStartTime,
  JobRuns.ReleaseTag as ReleaseTag,
  JobRuns.Cluster as Cluster,
  Jobs.Platform as Platform,
  Jobs.Architecture as Architecture,
  Jobs.Network as Network,
  Jobs.IPMode as IPMode,
  Jobs.Topology as Topology,
  Jobs.Release as Release,
  Jobs.FromRelease as FromRelease,
  if(Jobs.FromRelease="",false,true) as IsUpgrade,
FROM openshift-ci-data-analysis.ci_data.BackendDisruption
INNER JOIN openshift-ci-data-analysis.ci_data.BackendDisruption_JobRuns as JobRuns on BackendDisruption.JobRunName = JobRuns.Name
INNER JOIN openshift-ci-data-analysis.ci_data.Jobs on JobRuns.JobName = Jobs.JobName
`
)

const BackendDisruptionTableName = "BackendDisruption"

type BackendDisruptionRow struct {
	BackendName        string
	DisruptionSeconds  int
	JobName            bigquery.NullString
	JobRunName         string
	JobRunStartTime    bigquery.NullTimestamp
	JobRunEndTime      bigquery.NullTimestamp
	Cluster            bigquery.NullString
	ReleaseTag         bigquery.NullString
	MasterNodesUpdated bigquery.NullString
	JobRunStatus       bigquery.NullString
}
