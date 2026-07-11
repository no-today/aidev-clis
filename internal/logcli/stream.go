package logcli

// StreamLines drives an Output from a line producer. If args contain
// `-f`/`--follow` it streams NDJSON live; otherwise it collects a Batch. Each
// line becomes {"message": line}. The caller's `produce` is invoked with an
// `emit` it calls once per output line.
//
// Adapters use this so the "stream-vs-batch by -f" decision lives in one place,
// without coupling the logcli package to any subprocess mechanism (produce is
// a plain closure).
func StreamLines(out Output, args []string, produce func(emit func(line string) error) error) error {
	follow := false
	for _, a := range args {
		if a == "-f" || a == "--follow" {
			follow = true
		}
	}
	if follow {
		s := out.Stream()
		return produce(func(line string) error { return s.Record(map[string]any{"message": line}) })
	}
	var recs []any
	if err := produce(func(line string) error {
		recs = append(recs, map[string]any{"message": line})
		return nil
	}); err != nil {
		return err
	}
	return out.Batch(recs)
}
