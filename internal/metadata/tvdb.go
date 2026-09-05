// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/nuxencs/seasonpackarr/pkg/errors"

	"github.com/autobrr/rls"
)

type tvdbClient struct {
	apiKey     string
	pin        string
	token      string
	expiresAt  time.Time
	httpClient *http.Client
}

type loginResponse struct {
	Data struct {
		Token string `json:"token"`
	} `json:"data"`
	Status string `json:"status"`
}

type searchResponse struct {
	Status string `json:"status"`
	Data   []struct {
		Name         string            `json:"name"`
		TvdbID       string            `json:"tvdb_id"`
		Year         string            `json:"year"`
		Aliases      []string          `json:"aliases,omitempty"`
		Translations map[string]string `json:"translations,omitempty"`
		RemoteIds    []struct {
			ID         string `json:"id"`
			Type       int    `json:"type"`
			SourceName string `json:"sourceName"`
		} `json:"remote_ids,omitempty"`
	} `json:"data"`
}

type seriesResponse struct {
	Data struct {
		DefaultSeasonType int `json:"defaultSeasonType"`
		Episodes          []struct {
			SeasonNumber int    `json:"seasonNumber"`
			SeriesID     int    `json:"seriesId"`
			SeasonName   string `json:"seasonName"`
			Year         string `json:"year"`
		} `json:"episodes"`
		Year string `json:"year"`
	} `json:"data"`
	Status string `json:"status"`
}

const (
	tvdbBaseURL = "https://api4.thetvdb.com/v4"
)

func newTVDB(apiKey, pin string) provider {
	return &tvdbClient{
		apiKey: apiKey,
		pin:    pin,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (t *tvdbClient) authenticate() error {
	// Check if token exists and is not expiring within the next day
	if t.token != "" && time.Now().Add(24*time.Hour).Before(t.expiresAt) {
		return nil
	}

	if t.apiKey == "" {
		return fmt.Errorf("tvdbAPIKey is not set")
	}

	loginRequest := make(map[string]string)

	if t.pin != "" {
		loginRequest["pin"] = t.pin
	}
	loginRequest["apikey"] = t.apiKey

	requestBody, err := json.Marshal(loginRequest)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, tvdbBaseURL+"/login", bytes.NewBuffer(requestBody))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to authenticate with tvdb: %s", resp.Status)
	}

	var loginResponse loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResponse); err != nil {
		return err
	}

	if loginResponse.Status != "success" {
		return fmt.Errorf("failed to authenticate with tvdb: %s", loginResponse.Status)
	}

	t.token = loginResponse.Data.Token
	t.expiresAt = time.Now().Add(30 * 24 * time.Hour)

	return nil
}

func (t *tvdbClient) getAuthHeader() (string, error) {
	if err := t.authenticate(); err != nil {
		return "", err
	}

	return fmt.Sprintf("Bearer %s", t.token), nil
}

func (t *tvdbClient) makeAPIRequest(endpoint string) ([]byte, error) {
	authHeader, err := t.getAuthHeader()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s%s", tvdbBaseURL, endpoint), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHeader)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	buf := bufio.NewReader(resp.Body)

	body, err := io.ReadAll(buf)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func (t *tvdbClient) searchSeries(query string, year int) (searchResponse, error) {
	resp, err := t.makeAPIRequest(fmt.Sprintf("/search?query=%s&type=series&year=%d", url.QueryEscape(query), year))
	if err != nil {
		return searchResponse{}, err
	}

	var searchResp searchResponse
	if err := json.NewDecoder(bytes.NewReader(resp)).Decode(&searchResp); err != nil {
		return searchResponse{}, err
	}

	if searchResp.Status != "success" {
		return searchResponse{}, fmt.Errorf("failed to get data from tvdb")
	}

	return searchResp, nil
}

func (t *tvdbClient) search(title string, year int) (string, error) {
	normTitle := rls.MustNormalize(title)

	searchResp, err := t.searchSeries(normTitle, year)
	if err != nil {
		return "", err
	}

	if len(searchResp.Data) > 0 {
		return searchResp.Data[0].TvdbID, nil
	}

	// the tvdb search endpoint regularly returns no results for long titles,
	// even when they exactly match a registered translation of a show; retry
	// with shortened title variants and only accept a show whose name, alias
	// or translation matches the original title
	for _, shortTitle := range shortenedTitles(normTitle) {
		searchResp, err = t.searchSeries(shortTitle, year)
		if err != nil {
			return "", err
		}

		for _, result := range searchResp.Data {
			candidates := append([]string{result.Name}, result.Aliases...)
			for _, translation := range result.Translations {
				candidates = append(candidates, translation)
			}

			if matchesTitle(normTitle, candidates...) {
				return result.TvdbID, nil
			}
		}
	}

	return "", fmt.Errorf("no matching search results")
}

// episodesInSeason returns the number of episodes in a season of a show.
func (t *tvdbClient) episodesInSeason(release rls.Release) (int, error) {
	tvdbID, err := t.search(release.Title, release.Year)
	if err != nil {
		return 0, fmt.Errorf("failed to find show %q on tvdb: %w", release.Title, err)
	}

	resp, err := t.makeAPIRequest(fmt.Sprintf("/series/%s/episodes/default?page=0&season=%d", tvdbID, release.Series))
	if err != nil {
		return 0, errors.Wrap(err, "failed to get episodes from tvdb")
	}

	var seriesResp seriesResponse
	if err := json.NewDecoder(bytes.NewReader(resp)).Decode(&seriesResp); err != nil {
		return 0, errors.Wrap(err, "failed to decode response from tvdb")
	}

	if seriesResp.Status != "success" {
		return 0, errors.Wrap(err, "failed to get data from tvdb")
	}

	if len(seriesResp.Data.Episodes) == 0 {
		return 0, fmt.Errorf("failed to find episodes in season %d of %q", release.Series, release.Title)
	}

	return len(seriesResp.Data.Episodes), nil
}
