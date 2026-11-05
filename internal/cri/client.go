// Package cri talks to the CRI API (https://cri.epita.fr/api/v2): campuses,
// classes and their members.
package cri

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"tp-inspector/internal/forge"
)

const BaseURL = "https://cri.epita.fr/api/v2"

// Client is a minimal CRI API client; Auth must request the "api" scope.
type Client struct {
	Auth *forge.Auth
	HTTP *http.Client
}

func NewClient(auth *forge.Auth) *Client {
	return &Client{Auth: auth, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

type Campus struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type Group struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Private bool   `json:"private"`
}

type user struct {
	Login string `json:"login"`
}

type page[T any] struct {
	Count   int    `json:"count"`
	Next    string `json:"next"`
	Results []T    `json:"results"`
}

// get performs an authenticated GET, retrying once on 401.
func (c *Client) get(u string, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := c.Auth.Token()
		if err != nil {
			return err
		}
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Accept", "application/json")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			c.Auth.Invalidate(tok)
			continue
		}
		if resp.StatusCode >= 300 {
			return fmt.Errorf("GET %s: %s: %.300s", u, resp.Status, body)
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("GET %s: %w", u, err)
		}
		return nil
	}
	return fmt.Errorf("GET %s: unauthorized", u)
}

// list follows the pagination of a list endpoint.
func list[T any](c *Client, path string, q url.Values) ([]T, error) {
	if q == nil {
		q = url.Values{}
	}
	q.Set("limit", "500")
	u := BaseURL + path + "?" + q.Encode()
	var out []T
	for u != "" {
		var p page[T]
		if err := c.get(u, &p); err != nil {
			return nil, err
		}
		out = append(out, p.Results...)
		u = p.Next
	}
	return out, nil
}

func (c *Client) Campuses() ([]Campus, error) {
	return list[Campus](c, "/campus/", nil)
}

// Groups lists the groups of a kind ("class", "semester"…).
func (c *Client) Groups(kind string) ([]Group, error) {
	return list[Group](c, "/groups/", url.Values{"kind": {kind}})
}

// Members returns the logins of a group's current members.
func (c *Client) Members(slug string) ([]string, error) {
	us, err := list[user](c, "/groups/"+url.PathEscape(slug)+"/members/", nil)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.Login
	}
	return out, nil
}
