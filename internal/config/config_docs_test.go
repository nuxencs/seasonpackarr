// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"encoding/json/v2"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/nuxencs/seasonpackarr/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The contract tests in this file check the checked-in files against the key tables, in both directions.

const composeExampleFile = "docker-compose.yml"

// envVariablePattern also matches a placeholder such as <NAME> in a variable name of a guide, in any case, so a
// failure message shows the whole name.
var envVariablePattern = regexp.MustCompile(envPrefix + `(?:[A-Z0-9_]|<[A-Za-z]+>)+`)

// declaredEnvironmentVariables returns each variable of the key tables, with clientName as the client name.
func declaredEnvironmentVariables(clientName string) []string {
	names := make([]string, 0, len(configEnvKeys)+len(clientEnvKeys))
	for _, key := range configEnvKeys {
		names = append(names, envPrefix+key.name)
	}
	for _, key := range clientEnvKeys {
		names = append(names, envPrefix+"CLIENTS_"+clientName+"_"+key.name)
	}
	return names
}

// namedEnvironmentVariables returns the set of SEASONPACKARR__ variables that the repository file names, comments included.
func namedEnvironmentVariables(t *testing.T, file string) map[string]bool {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("../..", file))
	require.NoError(t, err)
	named := map[string]bool{}
	for _, name := range envVariablePattern.FindAllString(string(contents), -1) {
		named[name] = true
	}
	require.NotEmpty(t, named, "%s names no %s variable", file, envPrefix)
	return named
}

func TestComposeExample_ListsEachDeclaredVariable(t *testing.T) {
	named := namedEnvironmentVariables(t, composeExampleFile)
	declared := declaredEnvironmentVariables("DEFAULT")

	for _, name := range declared {
		assert.True(t, named[name], "%s does not list %s; add it, as a commented-out line when few setups need it", composeExampleFile, name)
	}
	for _, name := range slices.Sorted(maps.Keys(named)) {
		assert.True(t, slices.Contains(declared, name), "%s names %s, which is not a declared variable", composeExampleFile, name)
	}
}

const configGuideFile = "docs/CONFIGURATION.md"

// configGuides are the guides that name the environment variables. A client variable names its client <NAME>.
var configGuides = []string{configGuideFile, "docs/PROWLARR.md"}

// guideUnknownVariableExamples are the typos that the configuration guide names to show the unknown-variable warning.
var guideUnknownVariableExamples = []string{envPrefix + "CLIENTS_DEFAULT_IMPORT_SAVEPATH", envPrefix + "LOG_LEVL"}

func TestConfigGuides_NameEachDeclaredVariable(t *testing.T) {
	named := map[string]string{}
	for _, guide := range configGuides {
		for name := range namedEnvironmentVariables(t, guide) {
			named[name] = guide
		}
	}

	for _, name := range declaredEnvironmentVariables("<NAME>") {
		_, ok := named[name]
		assert.True(t, ok, "neither of %v names %s", configGuides, name)
	}
	// The examples must stay unknown, or the guide would show a warning that never happens.
	require.Equal(t, guideUnknownVariableExamples, UnknownEnvironmentVariables(guideUnknownVariableExamples))
	for _, name := range guideUnknownVariableExamples {
		assert.Equal(t, configGuideFile, named[name], "%s no longer names the example %s; remove it from guideUnknownVariableExamples", configGuideFile, name)
	}
	for _, name := range slices.Sorted(maps.Keys(named)) {
		if name == envPrefix+"CLIENTS_<NAME>_<SETTING>" || slices.Contains(guideUnknownVariableExamples, name) {
			continue
		}
		// knownEnvironmentVariable also knows the CLI variables and the removed variables.
		known := knownEnvironmentVariable(strings.TrimPrefix(strings.ReplaceAll(name, "<NAME>", "DEFAULT"), envPrefix))
		assert.True(t, known, "%s names %s, which is not a declared, CLI, or removed variable", named[name], name)
	}
}

// guideRow is one row of a settings table, a table whose first column is "Setting".
type guideRow struct {
	// keys holds the dotted paths in the first column, with "*" for <name>.
	keys []string
	// defaultCell is the text of the "Default" column. hasDefault is false when the table has no such column.
	defaultCell string
	hasDefault  bool
}

var guideKeyPattern = regexp.MustCompile("`([^`]+)`")

func readGuideRows(t *testing.T) []guideRow {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("../..", configGuideFile))
	require.NoError(t, err)
	cells := func(line string) []string {
		parts := strings.Split(strings.Trim(line, "|"), "|")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}

	var rows []guideRow
	defaultColumn := -1
	inSettingsTable, inTable := false, false
	for line := range strings.Lines(string(contents)) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			inTable = false
			continue
		}
		row := cells(line)
		if !inTable {
			inTable = true
			inSettingsTable = row[0] == "Setting"
			defaultColumn = slices.Index(row, "Default")
			continue
		}
		if !inSettingsTable || strings.HasPrefix(row[0], "---") {
			continue
		}
		var parsed guideRow
		for _, match := range guideKeyPattern.FindAllStringSubmatch(row[0], -1) {
			parsed.keys = append(parsed.keys, strings.ReplaceAll(match[1], "<name>", "*"))
		}
		if defaultColumn >= 0 && defaultColumn < len(row) {
			parsed.defaultCell, parsed.hasDefault = row[defaultColumn], true
		}
		rows = append(rows, parsed)
	}
	require.NotEmpty(t, rows, "%s has no settings table", configGuideFile)
	return rows
}

// declaredKey is a YAML key of the key tables.
type declaredKey struct {
	typ reflect.Type
	// defaultValue is not valid when the key has no default tag.
	defaultValue reflect.Value
}

// declaredYAMLKeys maps each YAML key of the key tables, as a dotted path with "*" for the name of a client, to its
// declaration. A variable without a YAML key, such as DISABLE_CONFIG_FILE, is not in it.
func declaredYAMLKeys() map[string]declaredKey {
	keys := map[string]declaredKey{}
	add := func(root reflect.Type, prefix string, table envKeyTable) {
		for _, key := range table {
			path, typ := prefix, root
			for _, i := range key.index {
				field := typ.Field(i)
				name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
				path, typ = joinKey(path, name), field.Type
				if name == "-" {
					path = ""
					break
				}
			}
			if path != "" {
				keys[path] = declaredKey{typ: typ, defaultValue: key.defaultValue}
			}
		}
	}
	add(reflect.TypeFor[domain.Config](), "", configEnvKeys)
	add(reflect.TypeFor[domain.Client](), "clients.*", clientEnvKeys)
	return keys
}

// parseGuideDefault reads a Default cell: "empty", or one JSON value in backticks.
func parseGuideDefault(cell string, typ reflect.Type) (reflect.Value, error) {
	value := reflect.New(typ)
	if cell == "empty" {
		return value.Elem(), nil
	}
	text, opened := strings.CutPrefix(cell, "`")
	text, closed := strings.CutSuffix(text, "`")
	if !opened || !closed || strings.Contains(text, "`") {
		return reflect.Value{}, fmt.Errorf("is %q; write empty or one JSON value in backticks", cell)
	}
	if err := json.Unmarshal([]byte(text), value.Interface()); err != nil {
		return reflect.Value{}, fmt.Errorf("is %q, which is not a JSON value of type %s: %w", cell, typ, err)
	}
	return value.Elem(), nil
}

func TestConfigGuide_TablesMatchDeclaredKeys(t *testing.T) {
	declared := declaredYAMLKeys()
	known := serviceConfigDefaults()
	require.Contains(t, declared, "clients.*.import.savePath")

	documented := map[string]bool{}
	for _, row := range readGuideRows(t) {
		for _, key := range row.keys {
			documented[key] = true
			// A row can also name a group, such as clients.<name>.import.
			_, isKey := known[key]
			assert.True(t, isKey, "%s has a row for %s, which is not a config key", configGuideFile, key)
			decl, ok := declared[key]
			if !ok || !row.hasDefault {
				continue
			}
			got, err := parseGuideDefault(row.defaultCell, decl.typ)
			if !assert.NoError(t, err, "%s: the Default column of %s", configGuideFile, key) {
				continue
			}
			want := decl.defaultValue
			if !want.IsValid() {
				want = reflect.Zero(decl.typ)
			}
			// A nil list and an empty list are the same default.
			same := reflect.DeepEqual(got.Interface(), want.Interface()) ||
				(decl.typ.Kind() == reflect.Slice && got.Len() == 0 && want.Len() == 0)
			assert.True(t, same, "%s: the Default column of %s is %s, but the declared default is %v",
				configGuideFile, key, row.defaultCell, want.Interface())
		}
	}
	for _, key := range slices.Sorted(maps.Keys(declared)) {
		assert.True(t, documented[key], "%s has no table row for %s", configGuideFile, key)
	}
}
