// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package seasonpack

import (
	"errors"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutcome_Class(t *testing.T) {
	failure := errors.New("failure")
	tests := []struct {
		name    string
		outcome Outcome
		want    Class
	}{
		{name: "no error", outcome: Outcome{Status: domain.StatusSuccessfulMatch}, want: Accepted},
		{name: "status below 400", outcome: Outcome{Status: domain.StatusBelowThreshold, Err: failure}, want: Rejected},
		{name: "no reusable episode", outcome: Outcome{Status: domain.StatusFailedMatchToTorrentEps, Err: failure}, want: Rejected},
		{name: "hardlink failure", outcome: Outcome{Status: domain.StatusFailedHardlink, Err: failure}, want: Failed},
		{name: "client error", outcome: Outcome{Status: domain.StatusGetTorrentsError, Err: failure}, want: Failed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.outcome.Class())
		})
	}
}

// One processor serves each client and each release. No call may log with the
// fields of another call.
func TestProcessor_LogsReleaseAndClientOfEachCall(t *testing.T) {
	first := newPack(t, "First", 2, 1)
	second := newPack(t, "Second", 1, 1)
	// One client holds the episodes of both packs.
	first.client.Torrents = append(first.client.Torrents, second.client.Torrents...)
	first.client.FilesByHash["second"] = second.client.FilesByHash["ep1"]
	first.client.SavePathByHash = map[string]string{"second": second.sourceDir}
	first.client.Torrents[len(first.client.Torrents)-1].Hash = "second"
	f := newFixture(first.client)
	cfg := newConfig()
	other := cfg.Clients["default"].Clone()
	cfg.Clients["other"] = &other

	tests := []struct {
		pack   packFixture
		client string
	}{
		{pack: first, client: "default"},
		{pack: second, client: "other"},
	}
	for _, tt := range tests {
		f.log.Reset()
		client := f.client(t, tt.client, cfg)
		require.NoError(t, f.processor.Candidate(t.Context(), client, tt.pack.name).Err)
		require.NoError(t, f.processor.Match(t.Context(), client, tt.pack.name, TorrentBytes(tt.pack.torrent)).Err)
		require.NoError(t, f.processor.Import(t.Context(), client, tt.pack.name, tt.pack.torrent).Err)

		events := f.log.Events(t)
		require.NotEmpty(t, events)
		for _, message := range []string{"client release passed candidate gate", "season pack import plan built", "season pack import completed"} {
			events.Require(t, message)
		}
		for _, event := range events {
			assert.Equal(t, tt.pack.name, event["release"], "%v", event)
			assert.Equal(t, tt.client, event["clientname"], "%v", event)
			assert.Equal(t, "processor", event["module"], "%v", event)
		}
		f.log.AssertNoDuplicateKeys(t)
	}
}
