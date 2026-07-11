package tcli

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

type resolvedActor struct {
	Name     string // by name: actor name; inline: "inline-<hash>"
	FilePath string // inline: temp file path; else empty
}

// actorHash is a stable short hash of rendered vars (sorted k=v\n, sha256[:8]).
func actorHash(vars map[string]string) string {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{'='})
		h.Write([]byte(vars[k]))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

// parseRawHTTP parses a raw-HTTP request block (already template-rendered):
// first non-blank line is "METHOD PATH [HTTP/x]"; following lines up to the
// first blank line are "Header: value"; everything after the blank line is the
// body. Returns headers as "Name: value" strings ready for apicli -H.
func parseRawHTTP(block string) (method, path string, headers []string, body string) {
	lines := strings.Split(block, "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i < len(lines) {
		f := strings.Fields(strings.TrimSpace(lines[i]))
		if len(f) >= 1 {
			method = f[0]
		}
		if len(f) >= 2 {
			path = f[1]
		}
		i++
	}
	for ; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			i++ // consume the blank separator
			break
		}
		headers = append(headers, strings.TrimSpace(lines[i]))
	}
	if i < len(lines) {
		body = strings.TrimRight(strings.Join(lines[i:], "\n"), "\n")
	}
	return method, path, headers, body
}

// apiArgs builds the `apicli` argv (without the binary name) from a parsed
// request + resolved identity. Empty body -> no -d.
func apiArgs(app, method, path string, headers []string, body string, act resolvedActor, baseURL, saveBody, timeout string) []string {
	if method == "" {
		method = "GET"
	}
	args := []string{"call", app, path, "-X", method}
	for _, h := range headers {
		args = append(args, "-H", h)
	}
	if body != "" {
		args = append(args, "-d", body)
	}
	if act.Name != "" {
		args = append(args, "--actor", act.Name)
	}
	if act.FilePath != "" {
		args = append(args, "--actor-file", act.FilePath)
	}
	if baseURL != "" {
		args = append(args, "--base-url", baseURL)
	}
	if saveBody != "" {
		args = append(args, "--output-file", saveBody)
	}
	if timeout != "" {
		args = append(args, "--connect-timeout", timeout)
	}
	return append(args, "--output", "json")
}

// dbArgs builds the `dbcli` argv.
func dbArgs(driver, target, sql string, write bool, timeout string) []string {
	args := []string{driver, "--target", target}
	if write {
		args = append(args, "--allow-write")
	}
	if timeout != "" {
		args = append(args, "--timeout", timeout)
	}
	return append(args, sql)
}

// logArgs builds the `logcli` argv. sls uses trace/search; non-sls fetches a
// bounded logs slice (the client greps it). needle is already rendered.
func logArgs(adapter, target string, st *LogStep, needle string) []string {
	args := []string{adapter, "--target", target}
	if adapter == "sls" {
		if st.Trace != "" {
			args = append(args, "trace", needle)
		} else {
			args = append(args, "search", needle)
		}
		if st.From != "" {
			args = append(args, "--from", st.From)
		}
		if st.To != "" {
			args = append(args, "--to", st.To)
		}
		if st.Size > 0 {
			args = append(args, "--size", strconv.Itoa(st.Size))
		}
		return args
	}
	args = append(args, "logs")
	if st.Size > 0 {
		args = append(args, "--tail", strconv.Itoa(st.Size))
	}
	return args
}
