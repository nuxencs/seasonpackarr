// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"path/filepath"
	"testing"
)

// BenchmarkLoad measures one service config load of the repository config.yaml with environment overrides.
func BenchmarkLoad(b *testing.B) {
	configDir := b.TempDir()
	writeConfigFile(b, filepath.Join(configDir, "config.yaml"), defaultConfigFile)
	clearConnectionEnvironment(b)
	b.Setenv("SEASONPACKARR__PORT", "4242")
	b.Setenv("SEASONPACKARR__SMART_MODE", "true")
	b.Setenv("SEASONPACKARR__NOTIFICATIONS_NOTIFICATION_LEVEL", "MATCH,INFO")
	b.Setenv("SEASONPACKARR__CLIENTS_DEFAULT_IMPORT_TAGS", "a,b")
	b.Setenv("SEASONPACKARR__CLIENTS_DEFAULT_IMPORT_SAVE_PATH", configDir)

	for b.Loop() {
		if _, err := Load(configDir, "test"); err != nil {
			b.Fatal(err)
		}
	}
}
