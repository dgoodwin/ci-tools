package jobtableprimer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	v408 = "4.8"
	v409 = "4.9"
	v410 = "4.10"
	v411 = "4.11"
	v412 = "4.12"
	v413 = "4.13"
	v414 = "4.14"
	v415 = "4.15"
	v416 = "4.16"
)

func TestJobVersions(t *testing.T) {
	var testCases = []struct {
		name        string
		jobName     string
		fromVersion string
		toVersion   string
		shouldPanic bool
	}{
		{
			name:        "multi-version-upgrade",
			jobName:     "release-openshift-origin-installer-e2e-aws-upgrade-4.11-to-4.12-to-4.13-to-4.14-ci",
			fromVersion: "4.11",
			toVersion:   "4.14",
		},
		{
			name:        "non-upgrade",
			jobName:     "release-openshift-origin-installer-e2e-aws-4.11-to-4.12-to-4.13-to-4.14-ci",
			fromVersion: "",
			toVersion:   "4.14",
		},
		{
			name:        "non-upgrade-mixed-version-order",
			jobName:     "release-openshift-origin-installer-e2e-aws-4.11-to-4.12-to-4.14-to-4.13-ci",
			fromVersion: "",
			toVersion:   "4.14",
		},
		{
			name:        "micro-version-upgrade",
			jobName:     "release-openshift-origin-installer-e2e-aws-upgrade-4.14-ci",
			fromVersion: "4.14",
			toVersion:   "4.14",
		},
		{
			name:        "minor-version-upgrade",
			jobName:     "release-openshift-origin-installer-e2e-aws-upgrade-4.13-to-4.14-ci",
			fromVersion: "4.13",
			toVersion:   "4.14",
		},
		{
			name:        "missing-version-upgrade",
			jobName:     "release-openshift-origin-installer-e2e-aws-upgrade-ci",
			fromVersion: "",
			toVersion:   "unknown",
			shouldPanic: true,
		},
	}

	reverseOrderedVersions := []string{
		v416, v415, v414, v413, v412, v411, v410, v409, v408,
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.shouldPanic {
				assert.Panics(t, func() {
					newJob(tc.jobName, reverseOrderedVersions)
				}, "Expected panic for unknown release")
			} else {
				j := newJob(tc.jobName, reverseOrderedVersions)

				assert.NotNil(t, j, "Unexpected nil builder")
				assert.NotNil(t, j.job, "Unexpected nil job")
				assert.Equal(t, tc.toVersion, j.job.Release, "Invalid toVersion")
				assert.Equal(t, tc.fromVersion, j.job.FromRelease, "Invalid fromVersion")
			}
		})
	}

}
