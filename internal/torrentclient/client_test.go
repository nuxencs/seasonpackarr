// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	pkgerrors "github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testStackTracer interface {
	error
	StackTrace() pkgerrors.StackTrace
}

func TestImportErr_TracesOperationalFailuresOnly(t *testing.T) {
	sentinel := errors.New("client failed")
	operationalErr := importErr(ImportStageAdd, sentinel)

	require.ErrorIs(t, operationalErr, sentinel)
	_, traced := errors.AsType[testStackTracer](operationalErr)
	require.True(t, traced)

	configErr := importErr(ImportStageConfig, sentinel)
	_, traced = errors.AsType[testStackTracer](configErr)
	require.False(t, traced)
}

func TestNew_RejectsUnknownClientType(t *testing.T) {
	t.Parallel()

	_, err := New(t.Context(), &domain.Client{Type: "notarealclient"})
	require.EqualError(t, err, `type "notarealclient" is invalid - use "qbittorrent", "transmission", "deluge-v1", "deluge-v2" or "deluge"`)
}

func TestNew_RejectsInvalidConfigBeforeConnect(t *testing.T) {
	t.Parallel()
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	client := &domain.Client{Type: "transmission", Host: srv.URL, Import: domain.ImportPolicy{Category: "tv-hd"}}

	adapter, err := New(t.Context(), client)

	require.Error(t, err)
	assert.Equal(t, Validate(*client), err)
	assert.Nil(t, adapter)
	assert.Zero(t, requests.Load())
}

func TestEndpointOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b domain.Client
		same bool
	}{
		{
			name: "same type host and port",
			a:    domain.Client{Type: "transmission", Host: "10.0.0.1", Port: 9091, Username: "a", Inventory: domain.InventoryScope{IncludeCategories: []string{"tv"}}},
			b:    domain.Client{Type: "transmission", Host: "10.0.0.1", Port: 9091, Username: "b"},
			same: true,
		},
		{
			name: "empty type is qBittorrent",
			a:    domain.Client{Host: "qbit", Port: 8080},
			b:    domain.Client{Type: "qbittorrent", Host: "qbit", Port: 8080},
			same: true,
		},
		{
			name: "deluge alias is Deluge v2",
			a:    domain.Client{Type: "deluge", Host: "deluge", Port: 58846},
			b:    domain.Client{Type: "deluge-v2", Host: "deluge", Port: 58846},
			same: true,
		},
		{
			name: "Deluge v1 is not Deluge v2",
			a:    domain.Client{Type: "deluge-v1", Host: "deluge", Port: 58846},
			b:    domain.Client{Type: "deluge-v2", Host: "deluge", Port: 58846},
		},
		{
			name: "Deluge v1 is not the deluge alias",
			a:    domain.Client{Type: "deluge-v1", Host: "deluge", Port: 58846},
			b:    domain.Client{Type: "deluge", Host: "deluge", Port: 58846},
		},
		{
			name: "different type",
			a:    domain.Client{Type: "qbittorrent", Host: "seedbox", Port: 8080},
			b:    domain.Client{Type: "transmission", Host: "seedbox", Port: 8080},
		},
		{
			name: "different host",
			a:    domain.Client{Host: "qbit-a", Port: 8080},
			b:    domain.Client{Host: "qbit-b", Port: 8080},
		},
		{
			name: "different port",
			a:    domain.Client{Host: "qbit", Port: 8080},
			b:    domain.Client{Host: "qbit", Port: 8081},
		},
		{
			name: "Deluge port 0 is the daemon port",
			a:    domain.Client{Type: "deluge-v2", Host: "deluge", Port: 0},
			b:    domain.Client{Type: "deluge-v2", Host: "deluge", Port: 58846},
			same: true,
		},
		{
			name: "Deluge host name letter case",
			a:    domain.Client{Type: "deluge-v1", Host: "Deluge.LAN", Port: 58846},
			b:    domain.Client{Type: "deluge-v1", Host: "deluge.lan", Port: 58846},
			same: true,
		},
		{
			name: "port setting and port in host",
			a:    domain.Client{Host: "qbit", Port: 8080},
			b:    domain.Client{Host: "http://qbit:8080"},
			same: true,
		},
		{
			name: "port setting wins over port in host",
			a:    domain.Client{Host: "http://qbit:9090", Port: 8080},
			b:    domain.Client{Host: "qbit:8080"},
			same: true,
		},
		{
			name: "default scheme is http",
			a:    domain.Client{Host: "qbit:8080"},
			b:    domain.Client{Host: "http://qbit:8080"},
			same: true,
		},
		{
			name: "http default port",
			a:    domain.Client{Host: "http://qbit"},
			b:    domain.Client{Host: "qbit", Port: 80},
			same: true,
		},
		{
			name: "https default port",
			a:    domain.Client{Host: "https://qbit"},
			b:    domain.Client{Host: "https://qbit:443"},
			same: true,
		},
		{
			name: "host name letter case",
			a:    domain.Client{Host: "http://QBit.Example", Port: 8080},
			b:    domain.Client{Host: "http://qbit.example", Port: 8080},
			same: true,
		},
		{
			name: "last slash of a path",
			a:    domain.Client{Host: "http://qui:7476/proxy/abc/"},
			b:    domain.Client{Host: "http://qui:7476/proxy/abc"},
			same: true,
		},
		{
			name: "root path",
			a:    domain.Client{Host: "http://qbit:8080/"},
			b:    domain.Client{Host: "qbit", Port: 8080},
			same: true,
		},
		{
			name: "different qui proxy paths",
			a:    domain.Client{Host: "http://qui:7476/proxy/abc"},
			b:    domain.Client{Host: "http://qui:7476/proxy/def"},
		},
		{
			name: "Transmission path is not part of the endpoint",
			a:    domain.Client{Type: "transmission", Host: "http://transmission:9091/some/path"},
			b:    domain.Client{Type: "transmission", Host: "transmission", Port: 9091},
			same: true,
		},
		{
			name: "user info, query, and fragment are ignored",
			a:    domain.Client{Host: "http://user:secret@qbit:8080/?a=b#c"},
			b:    domain.Client{Host: "qbit", Port: 8080},
			same: true,
		},
		{
			name: "http is not https",
			a:    domain.Client{Host: "http://qbit:8080"},
			b:    domain.Client{Host: "https://qbit:8080"},
		},
		{
			name: "two spellings of one IPv6 address",
			a:    domain.Client{Type: "deluge-v2", Host: "[::1]", Port: 58846},
			b:    domain.Client{Type: "deluge-v2", Host: "[0:0:0:0:0:0:0:1]", Port: 58846},
			same: true,
		},
		{
			name: "IPv6 hex digit letter case",
			a:    domain.Client{Host: "http://[FD00::A]:8080"},
			b:    domain.Client{Host: "http://[fd00::a]:8080"},
			same: true,
		},
		{
			// Linux interface names are case sensitive, so "LAN" and "lan" can be two interfaces.
			name: "IPv6 zone letter case",
			a:    domain.Client{Type: "deluge-v2", Host: "[fe80::1%LAN]", Port: 58846},
			b:    domain.Client{Type: "deluge-v2", Host: "[fe80::1%lan]", Port: 58846},
		},
		{
			name: "host name is not its IP address",
			a:    domain.Client{Host: "localhost", Port: 8080},
			b:    domain.Client{Host: "127.0.0.1", Port: 8080},
		},
		{
			name: "address that does not build uses the trimmed host and port",
			a:    domain.Client{Type: "deluge-v2", Host: " http://deluge ", Port: 58846},
			b:    domain.Client{Type: "deluge-v2", Host: "http://deluge", Port: 58846},
			same: true,
		},
		{
			name: "address that does not build is not the built address",
			a:    domain.Client{Type: "deluge-v2", Host: "http://deluge", Port: 58846},
			b:    domain.Client{Type: "deluge-v2", Host: "deluge", Port: 58846},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.same {
				assert.Equal(t, EndpointOf(tt.a), EndpointOf(tt.b))
			} else {
				assert.NotEqual(t, EndpointOf(tt.a), EndpointOf(tt.b))
			}
		})
	}
}

func TestHTTPAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		client  *domain.Client
		want    string
		wantErr bool
	}{
		{
			name:    "empty host",
			client:  &domain.Client{Host: ""},
			want:    "",
			wantErr: true,
		},
		{
			name:   "bare hostname",
			client: &domain.Client{Host: "localhost"},
			want:   "http://localhost",
		},
		{
			name:   "bare hostname with port field",
			client: &domain.Client{Host: "localhost", Port: 8080},
			want:   "http://localhost:8080",
		},
		{
			name:   "hostname with http scheme",
			client: &domain.Client{Host: "http://localhost"},
			want:   "http://localhost",
		},
		{
			name:   "hostname with https scheme",
			client: &domain.Client{Host: "https://myhost"},
			want:   "https://myhost",
		},
		{
			name:   "hostname with scheme and port field",
			client: &domain.Client{Host: "https://myhost", Port: 8080},
			want:   "https://myhost:8080",
		},
		{
			name:   "hostname with existing port and port field override",
			client: &domain.Client{Host: "http://localhost:9090", Port: 8080},
			want:   "http://localhost:8080",
		},
		{
			name:   "schemeless host:port string",
			client: &domain.Client{Host: "localhost:9090"},
			want:   "http://localhost:9090",
		},
		{
			name:   "schemeless host:port with port field override",
			client: &domain.Client{Host: "localhost:9090", Port: 8080},
			want:   "http://localhost:8080",
		},
		{
			name:   "ip address",
			client: &domain.Client{Host: "192.168.1.1"},
			want:   "http://192.168.1.1",
		},
		{
			name:   "ip address with port field",
			client: &domain.Client{Host: "192.168.1.1", Port: 8080},
			want:   "http://192.168.1.1:8080",
		},
		{
			name:   "scheme with ip and port in host",
			client: &domain.Client{Host: "http://192.168.1.1:9090"},
			want:   "http://192.168.1.1:9090",
		},
		{
			name:   "zero port field does not append port",
			client: &domain.Client{Host: "http://localhost", Port: 0},
			want:   "http://localhost",
		},
		{
			name:   "host with path preserved",
			client: &domain.Client{Host: "http://localhost:8080/123456abcdef"},
			want:   "http://localhost:8080/123456abcdef",
		},
		{
			name:   "host with path and port field override",
			client: &domain.Client{Host: "http://localhost:8080/123456abcdef", Port: 9090},
			want:   "http://localhost:9090/123456abcdef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := httpAddress(*tt.client)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got.String())
		})
	}
}

func TestNew_CreatesTransmissionClient(t *testing.T) {
	t.Parallel()
	srv, _ := transmissionTestServer(t, map[string]string{"session-get": emptySessionResp})
	c, err := New(t.Context(), &domain.Client{Type: "transmission", Host: srv.URL})
	require.NoError(t, err)
	require.NotNil(t, c)
}
