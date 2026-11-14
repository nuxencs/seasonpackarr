// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

import (
	"reflect"
	"testing"

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
	collect(reflect.ValueOf(client).Elem(), "")
	return fields
}

// changeField gives a field a different value. It changes a list in place, so a copy that shares the list changes too.
func changeField(t *testing.T, name string, field reflect.Value) {
	t.Helper()

	switch field.Kind() {
	case reflect.String:
		require.NotEmptyf(t, field.String(), "newFullClient must set %s", name)
		field.SetString(field.String() + "-changed")
	case reflect.Int:
		require.NotZerof(t, field.Int(), "newFullClient must set %s", name)
		field.SetInt(field.Int() + 1)
	case reflect.Slice:
		require.NotZerof(t, field.Len(), "newFullClient must set %s", name)
		changeField(t, name+"[0]", field.Index(0))
	default:
		// A new kind of field needs a change here and a check in Client.Equal and Client.Clone.
		require.Failf(t, "unknown field kind", "changeField cannot change %s (%s)", name, field.Kind())
	}
}

var clientEqualSink bool

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

	withoutTags := newFullClient()
	withoutTags.Import.Tags = nil
	emptyTags := newFullClient()
	emptyTags.Import.Tags = []string{}
	require.True(t, withoutTags.Equal(emptyTags), "nil and empty tags have the same policy")

	withoutScope := newFullClient()
	withoutScope.Inventory = InventoryScope{}
	emptyScope := newFullClient()
	emptyScope.Inventory = InventoryScope{IncludeCategories: []string{}, IncludeTags: []string{}, ExcludeCategories: []string{}, ExcludeTags: []string{}}
	require.True(t, withoutScope.Equal(emptyScope), "nil and empty lists are the same scope")

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
