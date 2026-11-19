package main

// adapterVerbs returns the verbs a given (canonical) adapter accepts, for shell
// completion of the token after `logcli <adapter>`. Keep in sync with each
// adapter's Run switch; an unknown adapter yields nil (no verb suggestions).
func adapterVerbs(adapter string) []string {
	switch adapter {
	case "sls":
		return []string{"search", "trace", "tail", "doctor"}
	case "kubectl", "docker", "ssh-file", "ssh-docker", "local-file":
		return []string{"ls", "logs", "doctor"}
	default:
		return nil
	}
}

// adapterFlags returns adapter-level flags for shell completion after a verb.
// Keep in sync with adapter-specific parsers; logcli-level flags are prepended
// by logcliFlagsFor.
func adapterFlags(adapter, verb string) []string {
	switch adapter {
	case "sls":
		switch verb {
		case "search":
			return []string{"--from", "--to", "--size", "--reverse"}
		case "trace":
			return []string{"--from", "--to", "--size"}
		case "tail":
			return []string{"--interval"}
		default:
			return nil
		}
	default:
		return nil
	}
}

func logcliFlagsFor(adapter, verb string) []string {
	flags := []string{"--target", "--fields", "--config-dir", "--output", "--timeout", "--pretty"}
	return append(flags, adapterFlags(adapter, verb)...)
}
