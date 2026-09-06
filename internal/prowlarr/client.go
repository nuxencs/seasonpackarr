// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package prowlarr reads indexers, searches their Torznab feeds, and retrieves
// torrent bytes. It never sends a release to a Prowlarr download client.
package prowlarr

import (
	"cmp"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes = 32 << 20

// Indexer contains only discovery and search capabilities, never tracker secrets.
type Indexer struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	Enable             bool   `json:"enable"`
	Protocol           string `json:"protocol"`
	SupportsSearch     bool   `json:"supportsSearch"`
	SupportsPagination bool   `json:"supportsPagination"`
	Priority           int    `json:"priority"`
	Capabilities       struct {
		LimitsMax      int      `json:"limitsMax"`
		SearchParams   []string `json:"searchParams"`
		TvSearchParams []string `json:"tvSearchParams"`
		Categories     []struct {
			ID int `json:"id"`
		} `json:"categories"`
	} `json:"capabilities"`
}

// Result retains the opaque proxy URL only for retrieving the torrent.
type Result struct {
	Title     string `xml:"title"`
	GUID      string `xml:"guid"`
	Link      string `xml:"link"`
	Enclosure struct {
		URL string `xml:"url,attr"`
	} `xml:"enclosure"`
}

type Query struct {
	Title  string
	Year   int
	Season int
}

// String identifies the local search group for diagnostics and ordering.
// Tracker query text omits Year because releases may not contain it.
func (q Query) String() string {
	title := q.Title
	if q.Year > 0 {
		title += " " + strconv.Itoa(q.Year)
	}
	return fmt.Sprintf("%s S%02d", title, q.Season)
}

// CooldownError carries a retry deadline without exposing remote bodies or URLs.
type CooldownError struct {
	Until  time.Time
	reason string
}

func (e *CooldownError) Error() string { return e.reason }

func cooldown(reason, retryAfter string) error {
	now := time.Now()
	until := now.Add(10 * time.Minute)
	// Use the server deadline when valid; ten minutes is only a fallback.
	if seconds, err := strconv.ParseInt(strings.TrimSpace(retryAfter), 10, 64); err == nil && seconds >= 0 && seconds <= int64((1<<63-1)/time.Second) {
		until = now.Add(time.Duration(seconds) * time.Second)
	} else if deadline, err := http.ParseTime(retryAfter); err == nil {
		until = deadline
	}
	return &CooldownError{Until: until, reason: reason}
}

// Client is used serially by one backfill run. Rate spacing also covers downloads.
type Client struct {
	base        *url.URL
	apiKey      string
	http        *http.Client
	interval    time.Duration
	nextRequest time.Time
}

func New(rawURL, apiKey string, interval time.Duration) (*Client, error) {
	base, err := url.Parse(rawURL)
	if err != nil || base == nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("Prowlarr URL must be an HTTP(S) base URL without credentials, query, or fragment")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("Prowlarr API key is required")
	}
	if interval < 0 {
		return nil, errors.New("Prowlarr request interval cannot be negative")
	}
	return &Client{base: base, apiKey: apiKey, interval: interval, http: &http.Client{
		Timeout: 2 * time.Minute,
		// Prowlarr must proxy torrent bytes. Redirects can lead to a magnet or
		// expose its API key to a tracker, so report them without following them.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (c *Client) endpoint(path string, query url.Values) *url.URL {
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawPath = ""
	u.RawQuery = query.Encode()
	return &u
}

func (c *Client) Indexers(ctx context.Context) ([]Indexer, error) {
	data, err := c.get(ctx, c.endpoint("/api/v1/indexer", nil))
	if err != nil {
		return nil, err
	}
	var all []Indexer
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, errors.New("invalid Prowlarr indexer response")
	}
	selected := slices.DeleteFunc(all, func(i Indexer) bool { return !i.Enable || i.Protocol != "torrent" || !i.SupportsSearch })
	slices.SortFunc(selected, func(a, b Indexer) int { return cmp.Or(cmp.Compare(a.Priority, b.Priority), cmp.Compare(a.ID, b.ID)) })
	return selected, nil
}

// SearchPage prefers a structured TV season search, falling back to a text
// search when supported. Prowlarr's feed has no total count; callers stop on
// a short page, repeated results, or their page budget.
func (c *Client) SearchPage(ctx context.Context, indexer Indexer, q Query, offset int) ([]Result, int, error) {
	limit := 100
	if indexer.Capabilities.LimitsMax > 0 {
		limit = min(limit, indexer.Capabilities.LimitsMax)
	}
	params := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	// Some trackers expose only an Other category. Do not exclude their TV
	// packs through a category filter they do not advertise.
	for _, category := range indexer.Capabilities.Categories {
		if category.ID >= 5000 && category.ID < 6000 {
			params.Set("cat", "5000")
			break
		}
	}
	if slices.Contains(indexer.Capabilities.TvSearchParams, "q") && slices.Contains(indexer.Capabilities.TvSearchParams, "season") {
		params.Set("t", "tvsearch")
		params.Set("q", q.Title)
		params.Set("season", strconv.Itoa(q.Season))
	} else if slices.Contains(indexer.Capabilities.SearchParams, "q") {
		params.Set("t", "search")
		params.Set("q", fmt.Sprintf("%s S%02d", q.Title, q.Season))
	} else {
		return nil, limit, errors.New("indexer does not support title or season queries")
	}
	data, err := c.get(ctx, c.endpoint(fmt.Sprintf("/%d/api", indexer.ID), params))
	if err != nil {
		return nil, limit, err
	}
	var feed struct {
		XMLName xml.Name
		Code    string `xml:"code,attr"`
		Channel *struct {
			Items []Result `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, limit, errors.New("invalid Prowlarr search response")
	}
	if feed.XMLName.Local == "error" {
		return nil, limit, errors.New("Prowlarr returned a Torznab error")
	}
	if feed.XMLName.Local != "rss" || feed.Channel == nil {
		return nil, limit, errors.New("Prowlarr did not return a search feed")
	}
	return feed.Channel.Items, limit, nil
}

func (c *Client) Download(ctx context.Context, indexerID int, result Result) ([]byte, error) {
	raw := cmp.Or(result.Enclosure.URL, result.Link)
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		return nil, errors.New("result has no valid torrent download URL")
	}
	u = c.base.ResolveReference(u)
	// Accept only this indexer's Prowlarr proxy route. Never send the API key
	// to an origin or endpoint supplied by a search result.
	expected := strings.TrimRight(c.base.Path, "/") + fmt.Sprintf("/%d/download", indexerID)
	apiExpected := strings.TrimRight(c.base.Path, "/") + fmt.Sprintf("/api/v1/indexer/%d/download", indexerID)
	if u.Scheme != c.base.Scheme || !strings.EqualFold(u.Host, c.base.Host) || u.User != nil || (u.Path != expected && u.Path != apiExpected) {
		return nil, errors.New("result must use this Prowlarr indexer's torrent proxy; direct and magnet links are unsupported")
	}
	query := u.Query()
	query.Del("apikey")
	u.RawQuery = query.Encode()
	return c.get(ctx, u)
}

// get deliberately omits remote bodies and URLs from errors, since either can
// contain Prowlarr API keys or tracker passkeys.
func (c *Client) get(ctx context.Context, u *url.URL) ([]byte, error) {
	if wait := time.Until(c.nextRequest); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("could not create Prowlarr request")
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("User-Agent", "seasonpackarr")
	c.nextRequest = time.Now().Add(c.interval)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, cooldown("Prowlarr request failed or timed out", "")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, cooldown("Prowlarr returned HTTP 429; indexer is rate limited", resp.Header.Get("Retry-After"))
	}
	if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode >= 500 && resp.StatusCode <= 599 {
		return nil, cooldown(fmt.Sprintf("Prowlarr returned HTTP %d", resp.StatusCode), resp.Header.Get("Retry-After"))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Prowlarr returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, cooldown("could not read Prowlarr response", "")
	}
	if len(data) > maxResponseBytes {
		return nil, errors.New("Prowlarr response exceeds 32 MiB")
	}
	return data, nil
}
