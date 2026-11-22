// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package envname compares environment variable names as os.LookupEnv does: without case on Windows, exactly on other systems.
// Each package that reads an environment as a value uses it, so that their name matches cannot differ.
package envname

import (
	"runtime"
	"strings"
)

// Rule tells when two environment variable names are the same variable. The zero Rule compares names exactly.
type Rule struct {
	ignoreCase bool
}

var (
	// IgnoreCase is the rule of Windows. Tests use it to run that rule on each system.
	IgnoreCase = Rule{ignoreCase: true}
	// System is the rule of the current system.
	System = Rule{ignoreCase: runtime.GOOS == "windows"}
)

// Fold returns the form of name that the rule compares. Two names are the same variable when their folded forms are equal.
// Under IgnoreCase the folded form is in upper case, so it matches the upper-case names of seasonpackarr.
func (r Rule) Fold(name string) string {
	if r.ignoreCase {
		return strings.ToUpper(name)
	}
	return name
}

// Lookup returns the value of the first entry for key in environ ("KEY=value" entries), as os.LookupEnv does.
func (r Rule) Lookup(environ []string, key string) (string, bool) {
	key = r.Fold(key)
	for _, entry := range environ {
		if name, value, ok := strings.Cut(entry, "="); ok && r.Fold(name) == key {
			return value, true
		}
	}
	return "", false
}
