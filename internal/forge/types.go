package forge

import "time"

type Activity struct {
	URI        string `json:"uri"`
	Slug       string `json:"slug"`
	TenantSlug string `json:"tenantSlug"`
	Name       string `json:"name"`
}

type PickedSubmission struct {
	GroupSlug         string    `json:"groupSlug"`
	SubmissionID      string    `json:"submissionId"`
	CurrentJobID      string    `json:"currentJobId"`
	TracePresignedURL string    `json:"tracePresignedUrl"`
	SubmissionError   string    `json:"submissionError"`
	SubmissionStatus  string    `json:"submissionStatus"`
	ReceivedAt        time.Time `json:"receivedAt"`
}

type Job struct {
	ID             string `json:"id"`
	SubmissionID   string `json:"submissionId"`
	Status         string `json:"status"`
	SuccessPercent *int   `json:"successPercent"`
	IsCurrent      bool   `json:"isCurrent"`
}

type Submission struct {
	ID                      string    `json:"id"`
	Status                  string    `json:"status"`
	Error                   string    `json:"error"`
	Ref                     string    `json:"ref"`
	Repository              string    `json:"repository"`
	GroupSlug               string    `json:"groupSlug"`
	IP                      string    `json:"ip"`
	Author                  string    `json:"author"`
	ReceivedAt              time.Time `json:"receivedAt"`
	Published               bool      `json:"published"`
	Validated               bool      `json:"validated"`
	AssignmentURI           string    `json:"assignmentUri"`
	SubmissionDefinitionURI string    `json:"submissionDefinitionUri"`
	Jobs                    []Job     `json:"jobs"`
	CurrentJob              *Job      `json:"currentJob"`
}

// Percent returns the success percentage of the current job, or -1.
func (s Submission) Percent() int {
	if s.CurrentJob != nil && s.CurrentJob.SuccessPercent != nil {
		return *s.CurrentJob.SuccessPercent
	}
	for _, j := range s.Jobs {
		if j.IsCurrent && j.SuccessPercent != nil {
			return *j.SuccessPercent
		}
	}
	return -1
}

type exploreResponse struct {
	Count     int64        `json:"count"`
	PageCount int          `json:"pageCount"`
	Results   []Submission `json:"results"`
}

type Group struct {
	URI          string   `json:"uri"`
	Slug         string   `json:"slug"`
	Members      []string `json:"members"`
	GitRemoteURL string   `json:"gitRemoteUrl"`
}
