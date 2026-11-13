// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package http

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"
)

func TestCLI_CheckAndImportFromLocalConfig(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "seasonpackarr")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../..")
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
	for _, test := range []struct {
		name        string
		genericRoot bool
	}{{name: "embedded name"}, {name: "release override", genericRoot: true}} {
		t.Run(test.name, func(t *testing.T) {
			f := newProcessorHTTPFixture(t, 1, 1, 0.75)
			packName := f.releaseName
			if test.genericRoot {
				packName = "Lifecycle.S01"
				meta, err := metainfo.Load(bytes.NewReader(f.torrent))
				require.NoError(t, err)
				info, err := meta.UnmarshalInfo()
				require.NoError(t, err)
				info.Name = packName
				meta.InfoBytes, err = bencode.Marshal(info)
				require.NoError(t, err)
				var data bytes.Buffer
				require.NoError(t, meta.Write(&data))
				f.torrent = data.Bytes()
			}
			// Subprocess completion does not synchronize access to Go fixture memory.
			var requests sync.Mutex
			server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				requests.Lock()
				defer requests.Unlock()
				f.handler.ServeHTTP(w, r)
			}))
			defer server.Close()
			importCalls := func() int {
				requests.Lock()
				defer requests.Unlock()
				return f.mock.importCalls
			}
			u, err := url.Parse(server.URL)
			require.NoError(t, err)
			_, port, err := net.SplitHostPort(u.Host)
			require.NoError(t, err)

			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), fmt.Appendf(nil,
				"host: 0.0.0.0\nport: %s\napiToken: %s\nclients:\n  default: {}\n", port, processorTestToken), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "download.torrent"), f.torrent, 0o600))

			environment := cliEnvironment()
			run := func(wantExit int, args ...string) (string, string) {
				t.Helper()
				command := exec.CommandContext(t.Context(), binary, args...)
				command.Dir, command.Env = dir, environment
				var stdout, stderr bytes.Buffer
				command.Stdout, command.Stderr = &stdout, &stderr
				err := command.Run()
				if wantExit == 0 {
					require.NoError(t, err, stderr.String())
				} else {
					exit, ok := errors.AsType[*exec.ExitError](err)
					require.True(t, ok, "expected process failure, got %v", err)
					require.Equal(t, wantExit, exit.ExitCode(), stdout.String()+stderr.String())
				}
				t.Logf("%s\n%s%s", strings.Join(args, " "), stdout.String(), stderr.String())
				return stdout.String(), stderr.String()
			}

			_, stderr := run(1, "match")
			require.Contains(t, stderr, "<file.torrent>")
			stdout, _ := run(1, "candidate", f.releaseName, "--api", "wrong-token")
			require.Contains(t, stdout, "authentication failed")
			stdout, _ = run(2, "candidate", "Different.S01.1080p.WEB-DL.H.264-RlsGrp")
			require.Contains(t, stdout, "Candidate rejected")

			stdout, _ = run(0, "candidate", f.releaseName)
			require.Contains(t, stdout, "Candidate accepted")
			packArgs := []string{"download.torrent"}
			if test.genericRoot {
				stdout, _ = run(2, "match", "download.torrent")
				require.Contains(t, stdout, "no matching releases")
				packArgs = append(packArgs, "--release", f.releaseName)
			}
			stdout, _ = run(0, append([]string{"match"}, packArgs...)...)
			require.Contains(t, stdout, "Match accepted")
			require.Contains(t, stdout, f.releaseName)
			require.Zero(t, importCalls())
			entries, err := os.ReadDir(f.importDir)
			require.NoError(t, err)
			require.Empty(t, entries)

			stdout, _ = run(0, append([]string{"import"}, packArgs...)...)
			require.Contains(t, stdout, "torrent imported")
			require.Equal(t, 1, importCalls())
			file := "Lifecycle.S01E01.1080p.WEB-DL.H.264-RlsGrp.mkv"
			source, err := os.Stat(filepath.Join(f.sourceDir, file))
			require.NoError(t, err)
			target, err := os.Stat(filepath.Join(f.importDir, packName, file))
			require.NoError(t, err)
			require.True(t, os.SameFile(source, target))
		})
	}
}

func cliEnvironment() []string {
	var environment []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "SEASONPACKARR__") {
			environment = append(environment, entry)
		}
	}
	return environment
}
