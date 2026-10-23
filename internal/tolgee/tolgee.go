// Package tolgee reads projects from a Tolgee server through its REST API (v2), so they can be
// moved into LowLex without exporting files.
//
// Both kinds of Tolgee keys work: a project API key (tgpak_…) knows its project; a personal
// access token (tgpat_…) needs the project id.
package tolgee

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	BaseURL   string
	APIKey    string
	ProjectID int64
	HTTP      *http.Client
}

type Language struct {
	ID           int64  `json:"id"`
	Tag          string `json:"tag"`
	Name         string `json:"name"`
	OriginalName string `json:"originalName"`
	Base         bool   `json:"base"`
}

type Info struct {
	ProjectID   int64      `json:"project_id"`
	ProjectName string     `json:"project_name"`
	Description string     `json:"description"`
	BaseTag     string     `json:"base_language"`
	Languages   []Language `json:"languages"`
	Keys        int        `json:"keys"`
}

type Translation struct {
	Text  string `json:"text"`
	State string `json:"state"` // UNTRANSLATED, TRANSLATED, REVIEWED, DISABLED
}

type Key struct {
	ID           int64                  `json:"keyId"`
	Name         string                 `json:"keyName"`
	Namespace    *string                `json:"keyNamespace"`
	Description  *string                `json:"keyDescription"`
	Tags         []struct{ Name string } `json:"keyTags"`
	Translations map[string]Translation `json:"translations"`
}

type Page struct {
	Keys       []Key
	Total      int
	NextCursor string
	Number     int
	Pages      int
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

// Error is a non-2xx answer from Tolgee.
type Error struct {
	Status int
	Body   string
}

func (e *Error) Error() string {
	switch e.Status {
	case 401:
		return "Tolgee rejected the API key (401)"
	case 403:
		return "the API key cannot read this project's translations (403); it needs the translations.view scope"
	case 404:
		return "Tolgee answered 404: check the URL and the project id"
	}
	return fmt.Sprintf("Tolgee answered %d: %s", e.Status, e.Body)
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	base := strings.TrimRight(c.BaseURL, "/")
	u := base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var lastErr error
	for attempt := range 6 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(1<<attempt) * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("X-API-Key", c.APIKey)
		req.Header.Set("Accept", "application/json")
		resp, err := c.client().Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		// rate limits and server hiccups are retried; anything else is final
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastErr = &Error{resp.StatusCode, snippet(body)}
			continue
		}
		if resp.StatusCode >= 300 {
			return &Error{resp.StatusCode, snippet(body)}
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("unexpected answer from %s: %w", path, err)
		}
		return nil
	}
	return lastErr
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// resolveProject finds the project id of a project API key when none was given.
func (c *Client) resolveProject(ctx context.Context) error {
	if c.ProjectID > 0 {
		return nil
	}
	var cur struct {
		ProjectID int64 `json:"projectId"`
	}
	if err := c.get(ctx, "/v2/api-keys/current", nil, &cur); err != nil {
		var te *Error
		if errors.As(err, &te) && (te.Status == 400 || te.Status == 403 || te.Status == 404) {
			return errors.New("this key is not a project API key; enter the Tolgee project id")
		}
		return err
	}
	if cur.ProjectID == 0 {
		return errors.New("enter the Tolgee project id")
	}
	c.ProjectID = cur.ProjectID
	return nil
}

func (c *Client) project() string { return "/v2/projects/" + strconv.FormatInt(c.ProjectID, 10) }

// Inspect returns the project's name, languages and key count.
func (c *Client) Inspect(ctx context.Context) (*Info, error) {
	if err := c.resolveProject(ctx); err != nil {
		return nil, err
	}
	var p struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		Description  string `json:"description"`
		BaseLanguage *struct {
			Tag string `json:"tag"`
		} `json:"baseLanguage"`
	}
	if err := c.get(ctx, c.project(), nil, &p); err != nil {
		return nil, err
	}
	info := &Info{ProjectID: c.ProjectID, ProjectName: p.Name, Description: p.Description}
	if p.BaseLanguage != nil {
		info.BaseTag = p.BaseLanguage.Tag
	}
	langs, err := c.Languages(ctx)
	if err != nil {
		return nil, err
	}
	info.Languages = langs
	for _, l := range langs {
		if l.Base && info.BaseTag == "" {
			info.BaseTag = l.Tag
		}
	}
	page, err := c.Page(ctx, nil, "", 1, 0)
	if err != nil {
		return nil, err
	}
	info.Keys = page.Total
	return info, nil
}

func (c *Client) Languages(ctx context.Context) ([]Language, error) {
	var res struct {
		Embedded struct {
			Languages []Language `json:"languages"`
		} `json:"_embedded"`
	}
	if err := c.get(ctx, c.project()+"/languages", url.Values{"size": {"1000"}}, &res); err != nil {
		return nil, err
	}
	return res.Embedded.Languages, nil
}

// Page reads one page of keys with their translations. Tolgee pages with an opaque cursor;
// pageNum is the fallback for servers that do not return one.
func (c *Client) Page(ctx context.Context, langs []string, cursor string, size, pageNum int) (*Page, error) {
	if err := c.resolveProject(ctx); err != nil {
		return nil, err
	}
	q := url.Values{"size": {strconv.Itoa(size)}, "sort": {"keyId"}}
	for _, l := range langs {
		q.Add("languages", l)
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	} else if pageNum > 0 {
		q.Set("page", strconv.Itoa(pageNum))
	}
	var res struct {
		Embedded struct {
			Keys []Key `json:"keys"`
		} `json:"_embedded"`
		Page struct {
			TotalElements int `json:"totalElements"`
			TotalPages    int `json:"totalPages"`
			Number        int `json:"number"`
		} `json:"page"`
		NextCursor string `json:"nextCursor"`
	}
	if err := c.get(ctx, c.project()+"/translations", q, &res); err != nil {
		return nil, err
	}
	return &Page{
		Keys:       res.Embedded.Keys,
		Total:      res.Page.TotalElements,
		NextCursor: res.NextCursor,
		Number:     res.Page.Number,
		Pages:      res.Page.TotalPages,
	}, nil
}
