package update

import (
	"cmp"
	"strconv"
	"strings"
)

type version struct {
	core [3]int
	pre  string
}

// parseVersion accepts 1.2.3 and v1.2.3, with an optional -prerelease.
// Builds such as dev or edge are not versions.
func parseVersion(s string) (version, bool) {
	s = strings.TrimPrefix(s, "v")
	s, _, _ = strings.Cut(s, "+")
	s, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	var v version
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return version{}, false
		}
		v.core[i] = n
	}
	v.pre = pre
	return v, true
}

func (v version) compare(o version) int {
	for i := range v.core {
		if c := cmp.Compare(v.core[i], o.core[i]); c != 0 {
			return c
		}
	}
	// 1.2.0-rc.1 comes before 1.2.0.
	switch {
	case v.pre == o.pre:
		return 0
	case v.pre == "":
		return 1
	case o.pre == "":
		return -1
	}
	return strings.Compare(v.pre, o.pre)
}

// newer reports whether latest is a release after current.
func newer(latest, current string) bool {
	l, ok := parseVersion(latest)
	if !ok {
		return false
	}
	c, ok := parseVersion(current)
	return ok && l.compare(c) > 0
}

// IsRelease reports whether v names a release rather than a dev or edge build.
func IsRelease(v string) bool {
	_, ok := parseVersion(v)
	return ok
}
