package tcli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

type discoverer struct {
	runner CLIRunner
	env    []string
	db     map[string]string // target -> driver
	log    map[string]string // target -> adapter
}

func newDiscoverer(r CLIRunner, env []string) *discoverer {
	return &discoverer{runner: r, env: env}
}

type targetInfo struct {
	Name    string `json:"name"`
	Adapter string `json:"adapter"`
}

// loadTargets runs `<cli> targets`, parses {data:[{name,adapter}]} into a map.
func (d *discoverer) loadTargets(ctx context.Context, cli string) (map[string]string, error) {
	stdout, runErr := d.runner.Run(ctx, cli, []string{"targets"}, d.env)
	res, err := parseEnvelope(stdout, runErr)
	if err != nil {
		return nil, err
	}
	if !res.HasData {
		return nil, errs.From(&errs.Error{Code: res.ErrCode, Message: res.ErrMsg, Exit: errs.ExitConfig})
	}
	var infos []targetInfo
	if err := json.Unmarshal(res.Data, &infos); err != nil {
		return nil, errs.General("DISCOVER_PARSE", fmt.Sprintf("%s targets: %v", cli, err))
	}
	m := make(map[string]string, len(infos))
	for _, i := range infos {
		m[i.Name] = i.Adapter
	}
	return m, nil
}

func (d *discoverer) dbDriver(ctx context.Context, target string) (string, error) {
	if d.db == nil {
		m, err := d.loadTargets(ctx, "dbcli")
		if err != nil {
			return "", err
		}
		d.db = m
	}
	drv, ok := d.db[target]
	if !ok {
		return "", errs.Config("DB_TARGET_NOT_FOUND", fmt.Sprintf("db target %q not in dbcli config", target))
	}
	return drv, nil
}

func (d *discoverer) logAdapter(ctx context.Context, target string) (string, error) {
	if d.log == nil {
		m, err := d.loadTargets(ctx, "logcli")
		if err != nil {
			return "", err
		}
		d.log = m
	}
	ad, ok := d.log[target]
	if !ok {
		return "", errs.Config("LOG_TARGET_NOT_FOUND", fmt.Sprintf("log target %q not in logcli config", target))
	}
	return ad, nil
}
