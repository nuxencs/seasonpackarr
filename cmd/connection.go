// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package cmd

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/nuxencs/seasonpackarr/internal/config"

	"github.com/spf13/cobra"
)

type connectionOptions struct {
	configDir, url, host, token, client string
	port                                int
	json                                bool
}

func (o *connectionOptions) addFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.configDir, "config", "c", "", "read config.yaml from this directory")
	f.StringVar(&o.url, "url", "", "service base URL, including any proxy path")
	f.StringVarP(&o.host, "host", "i", "", "service host (default: config or 127.0.0.1)")
	f.IntVarP(&o.port, "port", "p", 0, "service port (default: config or 42069)")
	f.StringVarP(&o.token, "api", "a", "", "API token (default: environment or config)")
	f.StringVarP(&o.client, "client", "n", "", "client name (default: sole configured client; search: all)")
	f.BoolVar(&o.json, "json", false, "print results as JSON")
	cmd.MarkFlagsMutuallyExclusive("url", "host")
	cmd.MarkFlagsMutuallyExclusive("url", "port")
}

func (o *connectionOptions) resolve(cmd *cobra.Command, allClients bool) (*apiClient, string, error) {
	settings, err := config.ReadConnectionSettings(o.configDir)
	if err != nil {
		return nil, "", err
	}
	token := settings.APIToken
	if cmd.Flags().Changed("api") {
		token = o.token
	}
	baseURL := os.Getenv("SEASONPACKARR__URL")
	if cmd.Flags().Changed("url") {
		baseURL = o.url
		if baseURL == "" {
			return nil, "", fmt.Errorf("--url requires an HTTP or HTTPS base URL")
		}
	}
	// Explicit host/port flags override an environment URL as a complete address.
	if baseURL == "" || cmd.Flags().Changed("host") || cmd.Flags().Changed("port") {
		host, port := settings.Host, settings.Port
		if cmd.Flags().Changed("host") {
			host = o.host
		}
		if cmd.Flags().Changed("port") {
			port = strconv.Itoa(o.port)
		}
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
		switch host {
		case "", "0.0.0.0":
			host = "127.0.0.1"
		case "::":
			host = "::1"
		}
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return nil, "", fmt.Errorf("service port must be an integer from 1 to 65535; set --port or correct SEASONPACKARR__PORT or the config")
		}
		if strings.ContainsAny(host, "/?#@") || (strings.Contains(host, ":") && net.ParseIP(host) == nil) {
			return nil, "", fmt.Errorf("host must be a hostname or IP address; use --url for a full URL")
		}
		baseURL = "http://" + net.JoinHostPort(host, strconv.Itoa(portNumber))
	}
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, "", fmt.Errorf("service URL must be an HTTP or HTTPS base URL without credentials, query parameters, or a fragment")
	}
	if value := u.Port(); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return nil, "", fmt.Errorf("service URL port must be from 1 to 65535")
		}
	}
	client := os.Getenv("SEASONPACKARR__CLIENT")
	if cmd.Flags().Changed("client") {
		client = o.client
	}
	if client == "" && !allClients {
		switch len(settings.Clients) {
		case 0:
			client = "default"
		case 1:
			client = settings.Clients[0]
		default:
			return nil, "", fmt.Errorf("select a client with --client; configured clients: %s", strings.Join(settings.Clients, ", "))
		}
	}
	return &apiClient{baseURL: strings.TrimRight(u.String(), "/"), token: token}, client, nil
}
