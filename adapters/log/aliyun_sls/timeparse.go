package aliyunsls

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

var reRelative = regexp.MustCompile(`^(\d+)([smhd])$`)

// ParseTime converts a human-friendly time string to unix seconds.
// Accepts: "now", "<N>s"/"<N>m"/"<N>h"/"<N>d" (relative past),
// raw unix seconds (>1e9), or RFC3339.
func ParseTime(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "now" || s == "" {
		return time.Now().Unix(), nil
	}

	// Raw unix seconds.
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 1_000_000_000 {
		return n, nil
	}

	// Relative.
	if m := reRelative.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		var dur time.Duration
		switch m[2] {
		case "s":
			dur = time.Duration(n) * time.Second
		case "m":
			dur = time.Duration(n) * time.Minute
		case "h":
			dur = time.Duration(n) * time.Hour
		case "d":
			dur = time.Duration(n) * 24 * time.Hour
		}
		return time.Now().Add(-dur).Unix(), nil
	}

	// RFC3339 absolute.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix(), nil
	}

	return 0, errs.General("TIME_PARSE_FAILED",
		fmt.Sprintf("cannot parse time %q (expected: 'now' | 30s/5m/2h/1d | unix seconds | RFC3339)", s))
}
