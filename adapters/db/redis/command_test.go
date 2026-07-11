package redis

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		cmd  string
		want Class
	}{
		{"GET", ClassRead}, {"get", ClassRead}, {"HGETALL", ClassRead}, {"SCAN", ClassRead},
		{"TTL", ClassRead}, {"EXISTS", ClassRead}, {"PING", ClassRead}, {"ZRANGE", ClassRead},
		{"GET.RO", ClassRead}, // .RO suffix family
		{"SET", ClassWrite}, {"DEL", ClassWrite}, {"EXPIRE", ClassWrite}, {"LPUSH", ClassWrite},
		{"HSET", ClassWrite}, {"INCR", ClassWrite},
		{"FLUSHALL", ClassAdmin}, {"FLUSHDB", ClassAdmin}, {"CONFIG", ClassAdmin},
		{"KEYS", ClassAdmin}, // moved from read → admin
		{"SHUTDOWN", ClassAdmin}, {"DEBUG", ClassAdmin}, {"SCRIPT", ClassAdmin}, {"EVAL", ClassAdmin},
		{"CLIENT", ClassAdmin},
	}
	for _, c := range cases {
		if got := Classify(c.cmd); got != c.want {
			t.Errorf("Classify(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}
