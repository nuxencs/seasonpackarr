package utils

import (
	"fmt"
	"golang.org/x/exp/slices"
	"regexp"
	"strings"

	"github.com/moistari/rls"
)

func GetFormattedTitle(r rls.Release) string {
	s := fmt.Sprintf("%s%d%d%s%s%s", rls.MustNormalize(r.Title), r.Year, r.Series,
		rls.MustNormalize(r.Resolution), rls.MustNormalize(r.Source), rls.MustNormalize(r.Group))

	slices.Sort(r.Cut)
	for _, a := range r.Cut {
		s += rls.MustNormalize(a)
	}

	slices.Sort(r.Edition)
	for _, a := range r.Edition {
		s += rls.MustNormalize(a)
	}

	slices.Sort(r.Other)
	for _, a := range r.Other {
		s += rls.MustNormalize(a)
	}

	slices.Sort(r.HDR)
	for _, a := range r.HDR {
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
	// regex for groups that don't need the folder name to be adjusted
	reIgnoredRlsGrps := regexp.MustCompile(`(?i)^(ZR)$`)

	reIllegal := regexp.MustCompile(`(?i)[\\/:"*?<>|]`)
	reAudio := regexp.MustCompile(`(?i)(AAC|DDP)\.(\d\.\d)`)
	reDots := regexp.MustCompile(`(?i)\.+`)

	r := rls.ParseString(packName)

	// check if RlsGrp of release is in ignore regex
	if !reIgnoredRlsGrps.MatchString(r.Group) {
		// remove illegal characters
		packName = reIllegal.ReplaceAllString(packName, "")
		// replace spaces with periods
		packName = strings.ReplaceAll(packName, " ", ".")
		// replace wrong audio naming
		packName = reAudio.ReplaceAllString(packName, "$1$2")
		// replace multiple dots with only one
		packName = reDots.ReplaceAllString(packName, ".")
	}
	return packName
}
