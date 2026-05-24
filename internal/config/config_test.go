package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/stretchr/testify/require"
)

func TestValidateDeprecatedConfigInputsRejectsParseTorrentFileInYAML(t *testing.T) {
	k := loadTestConfig(t, "parseTorrentFile: false\n")

	err := validateDeprecatedConfigInputs(k, nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "parseTorrentFile was removed")
	require.Contains(t, err.Error(), "see example config / README")
	require.Contains(t, err.Error(), "update your autobrr filter")
}

func TestValidateDeprecatedConfigInputsRejectsDeprecatedEnv(t *testing.T) {
	lookupEnv := func(key string) (string, bool) {
		if key == deprecatedParseTorrentFileEnv {
			return "false", true
		}

		return "", false
	}

	err := validateDeprecatedConfigInputs(nil, lookupEnv)

	require.Error(t, err)
	require.Contains(t, err.Error(), deprecatedParseTorrentFileEnv)
	require.Contains(t, err.Error(), "see example config / README")
	require.Contains(t, err.Error(), "update your autobrr filter")
}

func TestValidateDeprecatedConfigInputsRejectsPreImportPathInYAML(t *testing.T) {
	k := loadTestConfig(t, `
clients:
  default:
    preImportPath: /data/torrents/tv-hd
    qbit:
      category: tv-hd
`)

	err := validateDeprecatedConfigInputs(k, nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "clients.default.preImportPath")
	require.Contains(t, err.Error(), "was removed")
	require.Contains(t, err.Error(), "qbit.savePath or qbit.category")
}

func TestValidateDeprecatedConfigInputsRejectsPreImportPathEnv(t *testing.T) {
	t.Setenv("SEASONPACKARR__CLIENTS_DEFAULT_PREIMPORTPATH", "/data/torrents/tv-hd")

	err := validateDeprecatedConfigInputs(nil, os.LookupEnv)

	require.Error(t, err)
	require.Contains(t, err.Error(), "SEASONPACKARR__CLIENTS_DEFAULT_PREIMPORTPATH")
	require.Contains(t, err.Error(), "was removed")
	require.Contains(t, err.Error(), "qbit save path/category")
}

func TestValidateDeprecatedConfigInputsAllowsCurrentConfig(t *testing.T) {
	k := loadTestConfig(t, "host: 0.0.0.0\nclients: {}\n")

	err := validateDeprecatedConfigInputs(k, func(string) (string, bool) {
		return "", false
	})

	require.NoError(t, err)
}

func loadTestConfig(t *testing.T, content string) *koanf.Koanf {
	t.Helper()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o644))

	k := koanf.New(".")
	require.NoError(t, k.Load(file.Provider(configPath), yaml.Parser()))

	return k
}
