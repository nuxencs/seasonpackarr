package utils

import (
	"fmt"
	"github.com/moistari/rls"
	"regexp"
	"strings"
)

func GetFormattedTitle(r rls.Release) string {
	s := fmt.Sprintf("%s%d%d%s%s%s%s", rls.MustNormalize(r.Title), r.Year, r.Series,
		rls.MustNormalize(r.Resolution), rls.MustNormalize(r.Source),
		fmt.Sprintf("%s", r.HDR), r.Group)
	for _, a := range r.Cut {
		s += rls.MustNormalize(a)
	}

	for _, a := range r.Edition {
		s += rls.MustNormalize(a)
	}

	for _, a := range r.Other {
		s += rls.MustNormalize(a)
	}

	re := regexp.MustCompile(`(?i)(?:\d{3,4}p|Repack\d?|Proper\d?|Real)[-_. ](\w+)[-_. ]WEB`)
	service := re.FindStringSubmatch(fmt.Sprintf("%q", r))
	if len(service) > 1 {
		s += rls.MustNormalize(service[1])
	}

	return s
}

func FormatSeasonPackTitle(packName string) string {
	reIllegal := regexp.MustCompile(`(?i)[\\/:"*?<>|]`)
	reAudio := regexp.MustCompile(`(?i)(AAC|DDP)\.(\d\.\d)`)

	// remove illegal characters
	packName = reIllegal.ReplaceAllString(packName, "")
	// replace spaces with periods
	packName = strings.ReplaceAll(packName, " ", ".")
	// replace wrong audio naming
	packName = reAudio.ReplaceAllString(packName, "$1$2")

	return packName
}
