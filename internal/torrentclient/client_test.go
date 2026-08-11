// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
)

func requireImportFailure(t *testing.T, err error, wantReason domain.Reason, wantClass domain.FaultClass) {
	t.Helper()
	if err == nil {
		t.Fatal("expected import error")
	}

	outcome := ImportFailed(err)
	if validationErr := outcome.Validate(); validationErr != nil {
		t.Fatalf("invalid import outcome: %v", validationErr)
	}
	if outcome.Reason() != wantReason || outcome.FaultClass() != wantClass {
		t.Fatalf(
			"import failure = (%s, %s), want (%s, %s)",
			outcome.Reason(), outcome.FaultClass(), wantReason, wantClass,
		)
	}
}

func TestBuildTransmissionURL(t *testing.T) {
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
			wantErr: true,
		},
		{
			name:   "bare hostname appends rpc path",
			client: &domain.Client{Host: "localhost"},
			want:   "http://localhost/transmission/rpc",
		},
		{
			name:   "bare hostname with port field",
			client: &domain.Client{Host: "localhost", Port: 9091},
			want:   "http://localhost:9091/transmission/rpc",
		},
		{
			name:   "hostname with http scheme",
			client: &domain.Client{Host: "http://myhost"},
			want:   "http://myhost/transmission/rpc",
		},
		{
			name:   "hostname with https scheme",
			client: &domain.Client{Host: "https://myhost"},
			want:   "https://myhost/transmission/rpc",
		},
		{
			name:   "ip address with port field",
			client: &domain.Client{Host: "192.168.1.1", Port: 9091},
			want:   "http://192.168.1.1:9091/transmission/rpc",
		},
		{
			name:   "existing port overridden by port field",
			client: &domain.Client{Host: "http://localhost:8080", Port: 9091},
			want:   "http://localhost:9091/transmission/rpc",
		},
		{
			name:   "zero port field does not append port",
			client: &domain.Client{Host: "http://localhost", Port: 0},
			want:   "http://localhost/transmission/rpc",
		},
		{
			name:   "credentials embedded as user info",
			client: &domain.Client{Host: "localhost", Port: 9091, Username: "admin", Password: "secret"},
			want:   "http://admin:secret@localhost:9091/transmission/rpc",
		},
		{
			name:   "username only still embeds user info",
			client: &domain.Client{Host: "localhost", Username: "admin"},
			want:   "http://admin:@localhost/transmission/rpc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := buildTransmissionURL(tt.client)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tt.want {
				t.Errorf("buildTransmissionURL() = %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestNew_unknownType(t *testing.T) {
	t.Parallel()

	_, err := New(&domain.Client{Type: "notarealclient"})
	if err == nil {
		t.Fatal("expected error for unknown client type, got nil")
	}
	want := "unknown client type: notarealclient"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestBuildHost(t *testing.T) {
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
			got, err := buildHost(tt.client)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("buildHost() = %q, want %q", got, tt.want)
			}
		})
	}
}
