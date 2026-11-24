// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

import (
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const apiGuideFile = "../../docs/API.md"

// retiredStatusCodes are the numbers that docs/API.md names as retired. They must get no new meaning.
var retiredStatusCodes = []int{201, 202, 203, 204, 205, 206, 207, 208, 212, 213, 214, 215}

var statusRowPattern = regexp.MustCompile("^\\| `(\\d{3})` \\| [^|]+ \\| (?:`([A-Z]+)`|None)")

func readAPIGuide(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile(apiGuideFile)
	require.NoError(t, err)
	return string(contents)
}

// apiGuideStatusLevels reads the status table of docs/API.md as code to notification level, with "" for None.
func apiGuideStatusLevels(t *testing.T) map[int]string {
	t.Helper()
	_, section, found := strings.Cut(readAPIGuide(t), "\n## Status codes\n")
	require.True(t, found, "%s has no Status codes section", apiGuideFile)
	section, _, _ = strings.Cut(section, "\n## ")

	levels := map[int]string{}
	for line := range strings.SplitSeq(section, "\n") {
		match := statusRowPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		code, err := strconv.Atoi(match[1])
		require.NoError(t, err)
		levels[code] = match[2]
	}
	require.NotEmpty(t, levels, "%s has no status table rows", apiGuideFile)
	return levels
}

func notificationLevelOf(code StatusCode) string {
	for level, codes := range NotificationStatusMap {
		if slices.Contains(codes, code) {
			return level
		}
	}
	return ""
}

func TestStatusCode_MatchesAPIGuide(t *testing.T) {
	documented := apiGuideStatusLevels(t)

	for number := 100; number < 600; number++ {
		code := StatusCode(number)
		level, inGuide := documented[number]
		if code.String() == "" {
			assert.False(t, inGuide, "%s lists %d, which has no status text", apiGuideFile, number)
			assert.Empty(t, notificationLevelOf(code), "the notification level map holds %d, which has no status text", number)
			continue
		}
		if assert.True(t, inGuide, "%s does not list %d (%s)", apiGuideFile, number, code) {
			assert.Equal(t, notificationLevelOf(code), level, "notification level of %d in %s", number, apiGuideFile)
		}
	}
}

func TestStatusCode_RetiredCodesHaveNoMeaning(t *testing.T) {
	// Join the words, so a rewrap of the paragraph does not fail the test.
	guide := strings.Join(strings.Fields(readAPIGuide(t)), " ")
	assert.True(t, strings.Contains(guide, "`201` to `208` and `212` to `215` are retired"), "%s does not name the retired status codes", apiGuideFile)

	for _, number := range retiredStatusCodes {
		assert.Empty(t, StatusCode(number).String(), "retired status code %d has a text", number)
	}
}
