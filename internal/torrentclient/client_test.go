// Copyright (c) 2023 - 2025, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentclient

import (
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"
)

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
