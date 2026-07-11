package tcli

import "fmt"

// checkVars statically detects use-before-capture: it advances the available
// var set across setup -> steps -> assertions -> cleanup.
func (c *Case) checkVars() []string {
	avail := map[string]bool{"run_id": true, "uuid": true, "now": true, "trace_id": true}
	for k := range c.Vars {
		avail[k] = true
	}
	var d []string
	checkAction := func(phase string, i int, a Action) {
		for _, ref := range refsInAction(a) {
			if !avail[ref] {
				d = append(d, fmt.Sprintf("%s[%d] %q: uses {{%s}} before it is defined/captured", phase, i, a.Name, ref))
			}
		}
		for _, cap := range capturesInAction(a) {
			avail[cap] = true
		}
		if a.API != nil { // an api step may auto-capture trace_id (already seeded; harmless)
			avail["trace_id"] = true
		}
	}
	for _, ph := range []struct {
		name string
		acts []Action
	}{{"setup", c.Setup}, {"steps", c.Steps}, {"assertions", c.Assertions}, {"cleanup", c.Cleanup}} {
		for i, a := range ph.acts {
			checkAction(ph.name, i, a)
		}
	}
	return d
}

// refsInAction collects every {{var}} referenced by an action.
func refsInAction(a Action) []string {
	var refs []string
	add := func(s string) { refs = append(refs, TemplateVars(s)...) }
	addAll := func(ss []string) {
		for _, s := range ss {
			add(s)
		}
	}
	switch {
	case a.API != nil:
		add(a.API.Request)
		addAll(a.API.Expect)
	case a.DB != nil:
		add(a.DB.SQL)
		addAll(a.DB.Expect)
	case a.Log != nil:
		add(a.Log.Trace)
		add(a.Log.Query)
		addAll(a.Log.Expect)
	default:
		addAll(a.Expect)
	}
	return refs
}

// capturesInAction returns the capture keys a step defines (available afterward).
func capturesInAction(a Action) []string {
	var out []string
	collect := func(m map[string]string) {
		for k := range m {
			out = append(out, k)
		}
	}
	switch {
	case a.API != nil:
		collect(a.API.Capture)
	case a.DB != nil:
		collect(a.DB.Capture)
	case a.Log != nil:
		collect(a.Log.Capture)
	}
	return out
}
