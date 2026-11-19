package jcli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// ParamDef is one static parameter definition.
type ParamDef struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Default string   `json:"default,omitempty"`
	Choices []string `json:"choices,omitempty"`
}

// Params returns a job's static parameter definitions (reactive/Active-Choices
// options are NOT computed — see the spec).
func (c *Client) Params(ctx context.Context, job string) ([]ParamDef, error) {
	const tree = "property[parameterDefinitions[name,type,defaultParameterValue[value],choices]]"
	resp, err := c.do(ctx, http.MethodGet, jobPath(job)+"/api/json?tree="+tree, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errs.Remote("JOB_NOT_FOUND", "no such job: "+job)
	}
	var j struct {
		Property []struct {
			ParameterDefinitions []struct {
				Name                  string   `json:"name"`
				Type                  string   `json:"type"`
				Choices               []string `json:"choices"`
				DefaultParameterValue struct {
					Value any `json:"value"`
				} `json:"defaultParameterValue"`
			} `json:"parameterDefinitions"`
		} `json:"property"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&j); err != nil {
		return nil, errs.Remote("PARAMS_DECODE", err.Error())
	}
	var out []ParamDef
	for _, p := range j.Property {
		for _, d := range p.ParameterDefinitions {
			def := ""
			if d.DefaultParameterValue.Value != nil {
				def = fmt.Sprint(d.DefaultParameterValue.Value)
			}
			out = append(out, ParamDef{Name: d.Name, Type: normalizeParamType(d.Type), Default: def, Choices: d.Choices})
		}
	}
	return out, nil
}

func normalizeParamType(t string) string {
	switch {
	case strings.HasPrefix(t, "String"):
		return "string"
	case strings.HasPrefix(t, "Choice"):
		return "choice"
	case strings.HasPrefix(t, "Boolean"):
		return "boolean"
	case strings.HasPrefix(t, "Text"):
		return "text"
	case strings.HasPrefix(t, "Password"):
		return "password"
	default:
		return t
	}
}
