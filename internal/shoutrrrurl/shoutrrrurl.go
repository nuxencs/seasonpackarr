// Copyright (c) 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package shoutrrrurl names, validates, and redacts the Shoutrrr URLs of
// notifications.shoutrrr. It is apart from internal/notification, because
// internal/config validates the URLs and internal/notification imports internal/config.
package shoutrrrurl

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode"

	"github.com/nicholas-fedor/shoutrrr/pkg/router"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
)

const redacted = "[redacted]"

// Name names the Shoutrrr URL at index of notifications.shoutrrr by its list
// position and scheme, for example "shoutrrr[0] (gotify)". It holds no other part of the URL.
func Name(index int, rawURL string) string {
	return fmt.Sprintf("shoutrrr[%d] (%s)", index, scheme(rawURL))
}

// scheme returns the URL scheme, or "invalid scheme" when the text before
// "://" is not a scheme, so that the name never quotes other parts of the URL.
func scheme(rawURL string) string {
	before, _, found := strings.Cut(rawURL, "://")
	if !found || before == "" || !isScheme(before) {
		return "invalid scheme"
	}
	return strings.ToLower(before)
}

func isListSeparator(r rune) bool {
	return r == ',' || r == ';' || r == '|' || unicode.IsSpace(r)
}

// isScheme reports whether s is a URL scheme as RFC 3986 defines it.
func isScheme(s string) bool {
	for i, r := range s {
		switch {
		case 'a' <= r && r <= 'z', 'A' <= r && r <= 'Z':
		case i > 0 && ('0' <= r && r <= '9' || r == '+' || r == '-' || r == '.'):
		default:
			return false
		}
	}
	return true
}

// Validate parses rawURL with the Shoutrrr router, which also checks the
// settings of the service. The error has the secret parts of rawURL removed.
func Validate(rawURL string) error {
	serviceRouter, err := router.NewWithOptions(nil, types.SenderOptions{})
	if err != nil {
		return err
	}
	if _, err := serviceRouter.Locate(rawURL); err != nil {
		return errors.New(Redact(err.Error(), []string{rawURL}))
	}
	return nil
}

// minPartLength is the shortest path segment, query value, or list item that
// Redact removes alone. Shorter parts, such as "yes" or a chat ID "1", would
// garble status codes and words, and are too short to be a token.
const minPartLength = 4

// Redact removes from text the userinfo, the path, and the query of each URL in
// rawURLs, decoded and encoded. Service errors can quote these parts, and they
// can hold tokens. It also removes each path segment, query value, and list item
// of minPartLength or more, because services quote them one by one.
func Redact(text string, rawURLs []string) string {
	var secrets []string
	add := func(values ...string) {
		for _, value := range values {
			if value != "" {
				secrets = append(secrets, value, url.PathEscape(value), url.QueryEscape(value))
			}
		}
	}
	addParts := func(parts ...string) {
		for _, part := range parts {
			if len(part) >= minPartLength {
				add(part)
			}
		}
	}
	for _, rawURL := range rawURLs {
		add(rawURL)
		if _, rest, found := strings.Cut(rawURL, "://"); found {
			add(rest)
		}
		u, err := url.Parse(rawURL)
		if err != nil {
			continue
		}
		if u.User != nil {
			password, _ := u.User.Password()
			add(u.User.String(), u.User.Username(), password)
		}
		add(u.Opaque, u.Path, u.RawPath, u.EscapedPath(), u.RawQuery, u.Fragment)
		addParts(slices.Collect(strings.SplitSeq(u.Path, "/"))...)
		for _, values := range u.Query() {
			for _, value := range values {
				addParts(value)
				addParts(strings.FieldsFunc(value, isListSeparator)...)
			}
		}
	}
	if len(secrets) == 0 {
		return text
	}

	// Replace the longest part first, so that a part of it cannot leave the rest behind.
	slices.SortFunc(secrets, func(a, b string) int { return cmp.Or(cmp.Compare(len(b), len(a)), strings.Compare(a, b)) })
	secrets = slices.Compact(secrets)
	pairs := make([]string, 0, 2*len(secrets))
	for _, secret := range secrets {
		pairs = append(pairs, secret, redacted)
	}
	return strings.NewReplacer(pairs...).Replace(text)
}
