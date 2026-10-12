package forge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const BaseURL = "https://operator.forge.epita.fr"

// Client is a minimal operator API client.
type Client struct {
	Auth *Auth
	HTTP *http.Client
}

func NewClient(auth *Auth) *Client {
	return &Client{Auth: auth, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// GetRaw performs an authenticated GET and returns the body, retrying once on 401.
func (c *Client) GetRaw(path string, query url.Values) ([]byte, error) {
	return c.do("GET", path, query, nil)
}

func (c *Client) do(method, path string, query url.Values, body []byte) ([]byte, error) {
	u := BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := c.Auth.Token()
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequest(method, u, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			c.Auth.Invalidate(tok)
			continue
		}
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("%s %s: %s: %.300s", method, path, resp.Status, respBody)
		}
		return respBody, nil
	}
	return nil, fmt.Errorf("%s %s: unauthorized", method, path)
}

func (c *Client) getJSON(path string, query url.Values, out any) error {
	body, err := c.GetRaw(path, query)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	return nil
}

func esc(s string) string { return url.PathEscape(s) }

func (c *Client) Activities() ([]Activity, error) {
	var out []Activity
	return out, c.getJSON("/api/activities", nil, &out)
}

func (c *Client) SubmissionDefinitions(activityURI string) ([]string, error) {
	var out []string
	return out, c.getJSON("/api/activities/activity/"+esc(activityURI)+"/my-submissions-definitions", nil, &out)
}

// Assignment returns an assignment with the opening and closing times of its
// submission definitions.
func (c *Client) Assignment(assignmentURI string) (Assignment, error) {
	var out Assignment
	return out, c.getJSON("/api/activities/assignment/"+esc(assignmentURI), nil, &out)
}

func (c *Client) PickedLast(sdURI string) ([]PickedSubmission, error) {
	var out []PickedSubmission
	return out, c.getJSON("/api/traces/"+esc(sdURI)+"/picked/last", nil, &out)
}

func (c *Client) Groups(activityURI string) ([]Group, error) {
	var out []Group
	return out, c.getJSON("/api/activities/groups/"+esc(activityURI), nil, &out)
}

// SubmissionsPage fetches one page of explore-traces for an activity.
// pageNum must always be sent (the server 500s without it) and 100 is the max page size.
func (c *Client) SubmissionsPage(activityURI string, page int) (subs []Submission, pageCount int, err error) {
	q := url.Values{}
	q.Set("pageNum", fmt.Sprint(page))
	q.Set("pageSize", "100")
	var r exploreResponse
	if err := c.getJSON("/api/traces/"+esc(activityURI)+"/explore", q, &r); err != nil {
		return nil, 0, err
	}
	return r.Results, r.PageCount, nil
}

// GroupSubmissionsPage fetches one page of explore-traces of a single group.
func (c *Client) GroupSubmissionsPage(activityURI, groupSlug string, page int) (subs []Submission, pageCount int, err error) {
	q := url.Values{}
	q.Set("pageNum", fmt.Sprint(page))
	q.Set("pageSize", "100")
	q.Set("groupSlug", groupSlug)
	var r exploreResponse
	if err := c.getJSON("/api/traces/"+esc(activityURI)+"/explore", q, &r); err != nil {
		return nil, 0, err
	}
	return r.Results, r.PageCount, nil
}

// TraceURLs returns presigned trace URLs for any jobs of a submission
// definition (not only the picked ones), keyed by job id.
func (c *Client) TraceURLs(sdURI string, jobIDs []string) (map[string]string, error) {
	body, err := json.Marshal(jobIDs)
	if err != nil {
		return nil, err
	}
	b, err := c.do("PUT", "/api/traces/"+esc(sdURI)+"/job/traces-urls", nil, body)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	return out, json.Unmarshal(b, &out)
}

// Download fetches a (presigned, unauthenticated) URL.
func (c *Client) Download(u string) ([]byte, error) {
	resp, err := c.HTTP.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download: %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}
