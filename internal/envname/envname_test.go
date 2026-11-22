// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package envname

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRule_LookupReturnsTheFirstEntryWithTheName(t *testing.T) {
	environ := []string{"Xdg_Data_Home=mixed", "XDG_DATA_HOME=exact", "XDG_DATA_HOME=second", "EMPTY=", "NO_SEPARATOR"}
	tests := []struct {
		name string
		rule Rule
		want string
	}{
		{name: "exact", rule: Rule{}, want: "exact"},
		{name: "ignore case", rule: IgnoreCase, want: "mixed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, ok := tt.rule.Lookup(environ, "XDG_DATA_HOME")
			assert.True(t, ok)
			assert.Equal(t, tt.want, value)

			value, ok = tt.rule.Lookup(environ, "EMPTY")
			assert.True(t, ok)
			assert.Empty(t, value)

			_, ok = tt.rule.Lookup(environ, "XDG_CONFIG_HOME")
			assert.False(t, ok)
			_, ok = tt.rule.Lookup(environ, "NO_SEPARATOR")
			assert.False(t, ok)
		})
	}
}

func TestRule_FoldIsEqualForTheSameVariable(t *testing.T) {
	assert.Equal(t, "Seasonpackarr__Host", Rule{}.Fold("Seasonpackarr__Host"))
	assert.Equal(t, "SEASONPACKARR__HOST", IgnoreCase.Fold("Seasonpackarr__Host"))
}

// TestSystem_IsTheRuleOfOSLookupEnv: Windows environment names have no case, so os.LookupEnv ignores it there.
func TestSystem_IsTheRuleOfOSLookupEnv(t *testing.T) {
	want := Rule{}
	if runtime.GOOS == "windows" {
		want = IgnoreCase
	}
	assert.Equal(t, want, System)
}
