package jcli

import (
	"context"
	"regexp"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/core/exec"
)

// StepResult records a step that ran (completed steps are exit 0; a non-zero exit
// stops the flow with STEP_FAILED).
type StepResult struct {
	Argv []string `json:"argv"`
	Exit int      `json:"exit"`
}

// DeployResult is the deploy envelope payload.
type DeployResult struct {
	Job       string            `json:"job"`
	Build     int               `json:"build"`
	Result    string            `json:"result,omitempty"`
	URL       string            `json:"url"`
	Artifacts []Artifact        `json:"artifacts,omitempty"`
	Extracted map[string]string `json:"extracted,omitempty"`
	Steps     []StepResult      `json:"steps,omitempty"`
}

var defaultStepBinaries = map[string]bool{
	"kubectl": true, "ssh": true, "helm": true, "docker": true, "bash": true,
}

// RunDeploy runs the flow: trigger (deploy.params⊕cliParams) → wait → (if
// extract/steps) console → extract → allowlisted steps. wait=false stops after
// triggering. A non-SUCCESS build or any step failure stops the flow.
func RunDeploy(ctx context.Context, cl *Client, grp *Group, job, service string, cliParams map[string]string, wait bool) (DeployResult, error) {
	dep := grp.Deploy
	if dep == nil {
		dep = &DeployConfig{}
	}
	bt := &Templater{Service: service, Params: cliParams, Vars: grp.Vars}
	params := map[string]string{}
	for k, v := range dep.Params {
		ev, err := bt.Expand(v)
		if err != nil {
			return DeployResult{}, err
		}
		params[k] = ev
	}
	for k, v := range cliParams {
		params[k] = v // caller overrides a preset of the same name
	}

	res, err := cl.Trigger(ctx, job, params)
	if err != nil {
		return DeployResult{}, err
	}
	out := DeployResult{Job: job, Build: res.Build, URL: res.URL}
	if !wait {
		return out, nil
	}
	done, err := cl.Wait(ctx, job, res.Build)
	out.Result = done.Result
	if err != nil {
		return out, err // BUILD_FAILED — steps do not run
	}
	out.Artifacts, err = cl.Artifacts(ctx, job, res.Build)
	if err != nil {
		return out, err
	}
	if len(dep.Extract) == 0 && len(dep.Steps) == 0 {
		return out, nil
	}
	console, err := cl.Console(ctx, job, res.Build)
	if err != nil {
		return out, err
	}
	out.Extracted, err = runExtract(dep.Extract, service, console)
	if err != nil {
		return out, err
	}
	if len(dep.Steps) == 0 {
		return out, nil
	}
	// params, not cliParams: steps template against the same values sent to
	// Jenkins (deploy.params defaults ⊕ CLI overrides).
	out.Steps, err = runSteps(ctx, dep, grp.Vars, service, params, out.Extracted, out.Artifacts)
	return out, err
}

func runExtract(patterns map[string]string, service, console string) (map[string]string, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	t := &Templater{Service: service}
	out := map[string]string{}
	for name, pat := range patterns {
		expanded, err := t.ExpandRegex(pat)
		if err != nil {
			return nil, err
		}
		re, err := regexp.Compile(expanded)
		if err != nil {
			return nil, errs.Config("EXTRACT_PATTERN_INVALID", "extract "+name+": "+err.Error())
		}
		if re.NumSubexp() < 1 {
			return nil, errs.Config("EXTRACT_NO_CAPTURE", "extract "+name+" needs a capture group")
		}
		m := re.FindAllStringSubmatch(console, -1)
		if len(m) == 0 {
			return nil, errs.Remote("EXTRACT_NO_MATCH", "extract "+name+": no console match for "+expanded)
		}
		out[name] = m[len(m)-1][1] // last capture
	}
	return out, nil
}

func runSteps(ctx context.Context, dep *DeployConfig, vars map[string]string, service string, params, extracted map[string]string, artifacts []Artifact) ([]StepResult, error) {
	allow := defaultStepBinaries
	if len(dep.StepBinaries) > 0 {
		allow = map[string]bool{}
		for _, b := range dep.StepBinaries {
			allow[b] = true
		}
	}
	t := &Templater{Service: service, Params: params, Vars: vars, Extracts: extracted, Artifacts: artifacts}
	var env []string
	for k, v := range dep.StepEnv {
		ev, err := t.Expand(v)
		if err != nil {
			return nil, err
		}
		env = append(env, k+"="+ev)
	}
	var results []StepResult
	for i, raw := range dep.Steps {
		argv := make([]string, len(raw))
		for j, tok := range raw {
			ev, err := t.Expand(tok)
			if err != nil {
				return results, err
			}
			argv[j] = ev
		}
		if len(argv) == 0 {
			return results, errs.Config("STEP_EMPTY", "step "+itoa(i)+" has no command")
		}
		if !allow[argv[0]] {
			return results, errs.Config("STEP_BINARY_DENIED", "step "+itoa(i)+": binary "+argv[0]+" not in allowlist")
		}
		if err := (exec.Local{}).Run(ctx, exec.Spec{Argv: argv, Env: env}, func(string) error { return nil }); err != nil {
			return results, errs.Remote("STEP_FAILED", "step "+itoa(i)+" ("+argv[0]+") failed: "+errs.From(err).Message)
		}
		results = append(results, StepResult{Argv: argv, Exit: 0})
	}
	return results, nil
}
