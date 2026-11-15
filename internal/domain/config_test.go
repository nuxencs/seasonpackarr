// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFullClient returns a client config with a value in each field. Each call returns new slices.
func newFullClient() Client {
	return Client{
		Type:     "qbittorrent",
		Host:     "localhost",
		Port:     8080,
		Username: "user",
		Password: "password",
		APIKey:   "api-key",
		Import: ImportPolicy{
			SavePath:      "/save",
			Tags:          []string{"one", "two"},
			Category:      "category",
			DownloadPath:  "/download",
			ContentLayout: "subfolder",
		},
		Inventory: InventoryScope{
			IncludeCategories: []string{"tv", "tv.cross-seed"},
			IncludeTags:       []string{"keep"},
			ExcludeCategories: []string{"tv-4k"},
			ExcludeTags:       []string{"noreuse"},
		},
	}
}

// clientFields returns each field of client that is not a struct, by its dotted name.
func clientFields(client *Client) map[string]reflect.Value {
	return leafFields(reflect.ValueOf(client).Elem())
}

// leafFields returns each field of the struct value that is not a struct, by its dotted name.
func leafFields(value reflect.Value) map[string]reflect.Value {
	fields := make(map[string]reflect.Value)
	var collect func(value reflect.Value, prefix string)
	collect = func(value reflect.Value, prefix string) {
		for field, fieldValue := range value.Fields() {
			name := prefix + field.Name
			if fieldValue.Kind() == reflect.Struct {
				collect(fieldValue, name+".")
				continue
			}
			fields[name] = fieldValue
		}
	}
	collect(value, "")
	return fields
}

// changeField gives a field a different value. It changes a list, a map, and
// the value behind a pointer in place, so a copy that shares them changes too.
func changeField(t *testing.T, name string, field reflect.Value) {
	t.Helper()

	switch field.Kind() {
	case reflect.String:
		require.NotEmptyf(t, field.String(), "the full value must set %s", name)
		field.SetString(field.String() + "-changed")
	case reflect.Int:
		require.NotZerof(t, field.Int(), "the full value must set %s", name)
		field.SetInt(field.Int() + 1)
	case reflect.Bool:
		require.Truef(t, field.Bool(), "the full value must set %s", name)
		field.SetBool(false)
	case reflect.Float32:
		require.NotZerof(t, field.Float(), "the full value must set %s", name)
		field.SetFloat(field.Float() + 1)
	case reflect.Slice:
		require.NotZerof(t, field.Len(), "the full value must set %s", name)
		changeField(t, name+"[0]", field.Index(0))
	case reflect.Map:
		if field.Type().Key().Kind() != reflect.String || field.Type().Elem().Kind() != reflect.Pointer {
			failUnknownKind(t, name, field)
		}
		require.NotZerof(t, field.Len(), "the full value must set %s", name)
		for key, entry := range field.Seq2() {
			changeField(t, fmt.Sprintf("%s[%v]", name, key), entry)
		}
		field.SetMapIndex(reflect.ValueOf("added").Convert(field.Type().Key()), reflect.Zero(field.Type().Elem()))
	case reflect.Pointer:
		if field.Type().Elem().Kind() != reflect.Struct {
			failUnknownKind(t, name, field)
		}
		require.Falsef(t, field.IsNil(), "the full value must set %s", name)
		for fieldName, value := range leafFields(field.Elem()) {
			changeField(t, name+"."+fieldName, value)
		}
	default:
		failUnknownKind(t, name, field)
	}
}

// failUnknownKind names the copies to check, because a field that changeField cannot change is not guarded.
func failUnknownKind(t *testing.T, name string, field reflect.Value) {
	t.Helper()
	require.Failf(t, "unknown field kind", "changeField cannot change %s (%s): add the kind to changeField and check that Client.Equal, Client.Clone, and Config.Clone cover the field", name, field.Type())
}

func TestClientEqual_CoversEveryField(t *testing.T) {
	base := newFullClient()
	require.True(t, base.Equal(newFullClient()))

	for name := range clientFields(&base) {
		t.Run(name, func(t *testing.T) {
			changed := newFullClient()
			changeField(t, name, clientFields(&changed)[name])
			require.False(t, base.Equal(changed))
		})
	}

	tests := map[string]func(*Client){
		"tag added":         func(client *Client) { client.Import.Tags = append(client.Import.Tags, "other") },
		"include tag added": func(client *Client) { client.Inventory.IncludeTags = append(client.Inventory.IncludeTags, "other") },
		"exclude tags removed": func(client *Client) {
			client.Inventory.ExcludeTags = nil
		},
		"inventory scope removed": func(client *Client) { client.Inventory = InventoryScope{} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changed := newFullClient()
			mutate(&changed)
			require.False(t, base.Equal(changed))
		})
	}
}

func TestClientEqual_TreatsNilAndEmptyListsAsEqual(t *testing.T) {
	withoutTags := newFullClient()
	withoutTags.Import.Tags = nil
	emptyTags := newFullClient()
	emptyTags.Import.Tags = []string{}
	assert.True(t, withoutTags.Equal(emptyTags), "nil and empty tags have the same policy")

	withoutScope := newFullClient()
	withoutScope.Inventory = InventoryScope{}
	emptyScope := newFullClient()
	emptyScope.Inventory = InventoryScope{IncludeCategories: []string{}, IncludeTags: []string{}, ExcludeCategories: []string{}, ExcludeTags: []string{}}
	assert.True(t, withoutScope.Equal(emptyScope), "nil and empty lists are the same scope")
}

// clientEqualSink keeps the result, so the compiler cannot remove the measured call.
var clientEqualSink bool

func TestClientEqual_DoesNotAllocate(t *testing.T) {
	base := newFullClient()
	allocations := testing.AllocsPerRun(1000, func() {
		clientEqualSink = base.Equal(base)
	})
	require.Zero(t, allocations, "config equality is on the request path")
}

func TestClientClone_SharesNoMemory(t *testing.T) {
	base := newFullClient()
	clone := base.Clone()
	require.Equal(t, newFullClient(), clone)

	for name, field := range clientFields(&clone) {
		changeField(t, name, field)
	}
	require.Equal(t, newFullClient(), base, "a change to the copy must not change the original")
}

// newFullConfig returns a config with a value in each field. Each call returns new lists, maps, and clients.
func newFullConfig() Config {
	client := newFullClient()
	return Config{
		Version:            "v1.0.0",
		ConfigPath:         "/config",
		DisableConfigFile:  true,
		Host:               "0.0.0.0",
		Port:               42069,
		Clients:            map[string]*Client{"default": &client},
		LogPath:            "/config/logs/seasonpackarr.log",
		LogLevel:           "DEBUG",
		LogMaxSize:         50,
		LogMaxBackups:      3,
		SmartMode:          true,
		SmartModeThreshold: 0.75,
		FuzzyMatching: FuzzyMatching{
			SkipRepackCompare:  true,
			SimplifyHdrCompare: true,
			SimplifyWebCompare: true,
			SkipYearCompare:    true,
		},
		APIToken: "api-token",
		Notifications: Notifications{
			NotificationLevel: []string{"MATCH", "ERROR"},
			Discord:           "https://discord.example/webhook",
			Shoutrrr:          []string{"ntfy://ntfy.example/topic"},
		},
		Search: Search{
			IndexerIDs:      []int{1, 2},
			ProwlarrURL:     "http://prowlarr:9696",
			APIKey:          "prowlarr-api-key",
			RSSInterval:     "15m",
			RequestInterval: "2s",
		},
	}
}

// configFields returns each field of cfg that is not a struct, by its dotted name.
func configFields(cfg *Config) map[string]reflect.Value {
	return leafFields(reflect.ValueOf(cfg).Elem())
}

func TestConfigClone_SharesNoMemory(t *testing.T) {
	base := newFullConfig()
	require.Equal(t, newFullConfig(), base.Clone())

	for name := range configFields(&base) {
		t.Run(name, func(t *testing.T) {
			original := newFullConfig()
			clone := original.Clone()
			changeField(t, name, configFields(&clone)[name])
			require.Equal(t, newFullConfig(), original, "a change to the copy must not change the original")
		})
	}
}

func TestConfigClone_KeepsNilClientsAndAlwaysHasAClientMap(t *testing.T) {
	assert.NotNil(t, Config{}.Clone().Clients, "the copy always has a client map")

	clone := Config{Clients: map[string]*Client{"empty": nil}}.Clone()
	assert.Equal(t, map[string]*Client{"empty": nil}, clone.Clients, "a nil client stays nil")
}
