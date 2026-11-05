// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package autobrr

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
)

// externalTestTimeout is longer than the 120-second webhook timeout of
// autobrr, so a slow torrent client still gives its seasonpackarr status.
const externalTestTimeout = 130 * time.Second

// The results of a Verification.
const (
	VerificationPassed  = "passed"
	VerificationFailed  = "failed"
	VerificationSkipped = "skipped"
)

// Verification is the result of the candidate check that autobrr runs for a
// written filter.
type Verification struct {
	// Result is VerificationPassed, VerificationFailed, or VerificationSkipped.
	Result string `json:"result"`
	// Status is the HTTP status that autobrr got from the webhook URL.
	Status  int    `json:"status,omitzero"`
	Message string `json:"message"`
}

// VerifyCandidateCheck asks autobrr to run the candidate check entry of
// filter, so the request comes from the network position of autobrr. autobrr
// sends a single-episode sample release, so a pass is usually a rejection
// such as 211. The check needs autobrr v1.88.0 or later, and it is skipped on
// an older autobrr.
func (c *Client) VerifyCandidateCheck(ctx context.Context, filter Filter) Verification {
	var test struct {
		Status int    `json:"status"`
		Output string `json:"output"`
		Error  string `json:"error"`
	}
	tester := *c
	tester.http = &http.Client{Timeout: externalTestTimeout, CheckRedirect: c.http.CheckRedirect}
	err := tester.do(ctx, http.MethodPost, "/api/filters/external/test", filter.candidateCheck(), http.StatusOK, &test, filter.token)
	// The filter routes answered before, so a 404 means that autobrr has no test route.
	if status, ok := errors.AsType[*statusError](err); ok && status.status == http.StatusNotFound {
		return Verification{Result: VerificationSkipped, Message: "autobrr has no External test endpoint (added in autobrr v1.88.0)"}
	}
	if err != nil {
		return Verification{Result: VerificationFailed, Message: err.Error()}
	}
	failed := func(format string, args ...any) Verification {
		return Verification{Result: VerificationFailed, Status: test.Status, Message: fmt.Sprintf(format, args...)}
	}
	var answer struct {
		StatusCode int    `json:"statusCode"`
		Error      string `json:"error"`
	}
	fromSeasonpackarr := json.Unmarshal([]byte(test.Output), &answer) == nil && answer.StatusCode == test.Status
	client := c.safe(filter.client, filter.token)
	switch {
	// autobrr gives status 0 when the request fails, for example without a connection.
	case test.Status == 0 || test.Error != "":
		return failed("autobrr cannot reach the webhook URL: %s; check --webhook-url", c.detail(test.Error, filter.token))
	case test.Status == http.StatusUnauthorized:
		return failed("seasonpackarr rejected the token; check the seasonpackarr API token from --api, SEASONPACKARR__API_TOKEN, or apiToken")
	case test.Status == domain.StatusClientNotFound.Code():
		return failed("seasonpackarr does not know the client %s; check --client and the clients in the seasonpackarr config", client)
	case test.Status == domain.StatusGetClientError.Code() || test.Status == domain.StatusGetTorrentsError.Code():
		message := fmt.Sprintf("seasonpackarr cannot reach the torrent client %s", client)
		if answer.Error != "" {
			message += ": " + c.detail(answer.Error, filter.token)
		}
		return failed("%s; check the client settings in the seasonpackarr config", message)
	// A proxy or another service can answer at the webhook URL, so a pass needs a seasonpackarr answer.
	case test.Status == domain.StatusSuccessfulMatch.Code() || fromSeasonpackarr:
		return Verification{Result: VerificationPassed, Status: test.Status, Message: "the check reached seasonpackarr"}
	default:
		return failed("the webhook URL answered with HTTP %d and not with a seasonpackarr status; check --webhook-url", test.Status)
	}
}
