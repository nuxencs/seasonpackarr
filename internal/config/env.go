// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"errors"
	"fmt"
	"iter"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/nuxencs/seasonpackarr/internal/domain"
)

const envPrefix = "SEASONPACKARR__"

// envKey is one environment variable of a key table, read from the tags of its config field (see domain.Config).
type envKey struct {
	name  string
	index []int
	// set parses value into field, which has the type of the declared field.
	set func(field reflect.Value, value string) error
	// defaultValue is not valid when the field has no default tag.
	defaultValue reflect.Value
}

type envKeyTable []envKey

// The walks run once at package start and panic on a bad declaration, so every test binary of the package catches it.
var (
	configEnvKeys = envKeysOf(reflect.TypeFor[domain.Config]())
	clientEnvKeys = func() envKeyTable {
		keys := envKeysOf(reflect.TypeFor[domain.Client]())
		for _, key := range keys {
			// The file decode creates a client as a zero value, so a client default would apply only to some clients.
			if key.defaultValue.IsValid() {
				panic(fmt.Sprintf("config: client setting %s has a default tag; client settings have no defaults", key.name))
			}
		}
		return keys
	}()

	// envSettings maps each service variable, without envPrefix, to the field that it sets. UnknownEnvironmentVariables reads the same table.
	envSettings = setters[domain.Config](configEnvKeys)
	// clientEnvSettings maps each setting of a SEASONPACKARR__CLIENTS_<NAME>_<SETTING> variable to the client field that it sets.
	clientEnvSettings = setters[domain.Client](clientEnvKeys)

	// configDefaults holds the default tags. defaultConfig copies it.
	configDefaults = func() domain.Config {
		var cfg domain.Config
		root := reflect.ValueOf(&cfg).Elem()
		for _, key := range configEnvKeys {
			if key.defaultValue.IsValid() {
				root.FieldByIndex(key.index).Set(key.defaultValue)
			}
		}
		return cfg
	}()
)

// envKeysOf walks the fields of the struct type t. Struct and map fields are groups and have no env tag.
// Each other field has an env tag, and env:"-" marks a field that no variable sets.
func envKeysOf(t reflect.Type) envKeyTable {
	var keys envKeyTable
	var walk func(t reflect.Type, index []int)
	walk = func(t reflect.Type, index []int) {
		for field := range t.Fields() {
			where := t.String() + "." + field.Name
			name, tagged := field.Tag.Lookup("env")
			fieldIndex := slices.Concat(index, field.Index)
			switch field.Type.Kind() {
			case reflect.Struct, reflect.Map:
				if tagged {
					panic(fmt.Sprintf("config: group field %s has an env tag; tag its fields instead", where))
				}
				// A map holds named entries, such as the clients, which have a key table of their own.
				if field.Type.Kind() == reflect.Struct {
					walk(field.Type, fieldIndex)
				}
				continue
			}
			if !tagged || name == "" {
				panic(fmt.Sprintf("config: field %s has no env tag", where))
			}
			_, hasDefault := field.Tag.Lookup("default")
			_, hasSep := field.Tag.Lookup("sep")
			if name == "-" {
				if hasDefault || hasSep {
					panic(fmt.Sprintf("config: field %s has env:\"-\" and a default or sep tag", where))
				}
				continue
			}
			if slices.ContainsFunc(keys, func(key envKey) bool { return key.name == name }) {
				panic(fmt.Sprintf("config: field %s repeats the env name %s", where, name))
			}
			set, err := kindParser(field.Type, field.Tag.Get("sep"), hasSep)
			if err != nil {
				panic(fmt.Sprintf("config: field %s %v", where, err))
			}
			key := envKey{name: name, index: fieldIndex, set: set}
			if text, ok := field.Tag.Lookup("default"); ok {
				key.defaultValue = reflect.New(field.Type).Elem()
				if err := set(key.defaultValue, text); err != nil {
					panic(fmt.Sprintf("config: default tag of field %s %v", where, err))
				}
			}
			keys = append(keys, key)
		}
	}
	walk(t, nil)
	return keys
}

// setters returns a setter of each key's field in T. T must be the type that the table was walked from.
// It is a function, not a generic method, because gofumpt cannot parse generic methods yet.
func setters[T any](keys envKeyTable) map[string]func(*T, string) error {
	set := make(map[string]func(*T, string) error, len(keys))
	for _, key := range keys {
		set[key.name] = func(dst *T, value string) error {
			return key.set(reflect.ValueOf(dst).Elem().FieldByIndex(key.index), value)
		}
	}
	return set
}

// kindParser returns the parser of the field type. The sep tag selects the list separator: a comma when absent, or "whitespace".
func kindParser(t reflect.Type, sep string, hasSep bool) (func(reflect.Value, string) error, error) {
	split := func(value string) iter.Seq[string] { return strings.SplitSeq(value, ",") }
	switch {
	case !hasSep:
	case sep != "whitespace":
		return nil, fmt.Errorf("has the sep tag %q; use \"whitespace\" or no sep tag for a comma", sep)
	case t.Kind() != reflect.Slice:
		return nil, errors.New("has a sep tag but is not a list")
	default:
		split = strings.FieldsSeq
	}
	switch t {
	case reflect.TypeFor[string]():
		return fieldSetter(parseString), nil
	case reflect.TypeFor[bool]():
		return fieldSetter(parseBool), nil
	case reflect.TypeFor[int]():
		return fieldSetter(parseInt), nil
	case reflect.TypeFor[float32]():
		return fieldSetter(parseFloat32), nil
	case reflect.TypeFor[[]string]():
		return fieldSetter(func(value string) ([]string, error) { return parseList(split(value)) }), nil
	case reflect.TypeFor[[]int]():
		return fieldSetter(func(value string) ([]int, error) { return parseIntList(split(value)) }), nil
	}
	return nil, fmt.Errorf("has the type %s, which has no parser", t)
}

// fieldSetter writes through the field pointer, because a reflect.ValueOf of the parsed value allocates on each load.
func fieldSetter[T any](parse func(string) (T, error)) func(reflect.Value, string) error {
	return func(field reflect.Value, value string) error {
		v, err := parse(value)
		if err != nil {
			return err
		}
		*field.Addr().Interface().(*T) = v
		return nil
	}
}

// The kind parsers check syntax only; validateConfig checks the ranges. Their errors omit the value, which can be a secret.

func parseString(value string) (string, error) {
	if strings.ContainsFunc(value, unicode.IsControl) {
		return "", errors.New("contains a control character, such as a line break")
	}
	return value, nil
}

func parseBool(value string) (bool, error) {
	b, err := strconv.ParseBool(value)
	if err != nil {
		return false, errors.New("is not a boolean; use true or false")
	}
	return b, nil
}

func parseInt(value string) (int, error) {
	i, err := strconv.Atoi(value)
	if err != nil {
		return 0, errors.New("is not an integer")
	}
	return i, nil
}

func parseFloat32(value string) (float32, error) {
	f, err := strconv.ParseFloat(value, 32)
	if err != nil {
		return 0, errors.New("is not a number")
	}
	return float32(f), nil
}

// parseList returns the trimmed, non-empty entries. No entries is an error, because an empty list would replace the list of config.yaml.
func parseList(entries iter.Seq[string]) ([]string, error) {
	var list []string
	for entry := range entries {
		if entry = strings.TrimSpace(entry); entry == "" {
			continue
		}
		entry, err := parseString(entry)
		if err != nil {
			return nil, err
		}
		list = append(list, entry)
	}
	if len(list) == 0 {
		return nil, errors.New("has no entries; set at least one value or remove the variable")
	}
	return list, nil
}

func parseIntList(entries iter.Seq[string]) ([]int, error) {
	texts, err := parseList(entries)
	if err != nil {
		return nil, err
	}
	list := make([]int, len(texts))
	for i, text := range texts {
		if list[i], err = parseInt(text); err != nil {
			return nil, errors.New("has an entry that is not an integer")
		}
	}
	return list, nil
}

// cliEnvVariables are read by CLI commands in cmd, not by the service config.
var cliEnvVariables = []string{"URL", "CLIENT", "AUTOBRR_URL", "AUTOBRR_API_KEY"}

// disableConfigFileEnv has no config field, so the key tables do not know it. configFileDisabled reads it.
const disableConfigFileEnv = "DISABLE_CONFIG_FILE"

// cutClientEnvVariable splits CLIENTS_<NAME>_<SETTING> on the first underscore
// after the prefix, so multi-word settings such as IMPORT_SAVE_PATH stay whole.
func cutClientEnvVariable(name string) (clientName, setting string, ok bool) {
	after, ok := strings.CutPrefix(name, "CLIENTS_")
	if !ok {
		return "", "", false
	}
	return strings.Cut(after, "_")
}

func applyEnvironment(cfg *domain.Config, env environment) error {
	return applyEnvironmentSettings(cfg, env, envSettings, clientEnvSettings)
}

// applyEnvironmentSettings applies the variables that the maps name and joins an error for each value that does not parse.
func applyEnvironmentSettings(
	cfg *domain.Config,
	env environment,
	settings map[string]func(*domain.Config, string) error,
	clientSettings map[string]func(*domain.Client, string) error,
) error {
	var errs []error
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		name, ok := strings.CutPrefix(key, envPrefix)
		if !ok || value == "" {
			continue
		}
		var err error
		if set, ok := settings[name]; ok {
			err = set(cfg, value)
		} else if clientName, setting, ok := cutClientEnvVariable(name); ok && clientName != "" {
			// An unknown variable only logs a warning, so it must not add an empty client that fails validation.
			set, known := clientSettings[setting]
			if !known {
				continue
			}
			// The client is created here when absent, so clients defined only
			// through environment variables work too.
			clientName = strings.ToLower(clientName)
			if cfg.Clients[clientName] == nil {
				cfg.Clients[clientName] = &domain.Client{}
			}
			err = set(cfg.Clients[clientName], value)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("environment variable %s %w", key, err))
		}
	}
	return errors.Join(errs...)
}

// UnknownEnvironmentVariables returns the sorted SEASONPACKARR__ variables in
// environ ("KEY=value" entries) that neither the service config nor a CLI command
// reads. It ignores the values, so an empty unknown variable is reported too.
func UnknownEnvironmentVariables(environ []string) []string {
	var unknown []string
	for _, env := range environ {
		key, _, _ := strings.Cut(env, "=")
		name, ok := strings.CutPrefix(key, envPrefix)
		if ok && !knownEnvironmentVariable(name) {
			unknown = append(unknown, key)
		}
	}
	slices.Sort(unknown)
	return unknown
}

// knownEnvironmentVariable includes the removed variables, because
// validateDeprecatedConfigInputs stops startup with a migration error for them.
func knownEnvironmentVariable(name string) bool {
	if _, ok := envSettings[name]; ok || name == disableConfigFileEnv || slices.Contains(cliEnvVariables, name) {
		return true
	}
	if slices.Contains([]string{deprecatedParseTorrentFileEnv, deprecatedMetadataTVDBAPIKeyEnv, deprecatedMetadataTVDBPINEnv}, envPrefix+name) {
		return true
	}
	clientName, setting, ok := cutClientEnvVariable(name)
	_, known := clientEnvSettings[setting]
	return ok && clientName != "" && (known || setting == deprecatedPreImportPathEnv)
}
