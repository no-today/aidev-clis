package dataease

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/no-today/aidev-clis/internal/core/errs"
	"github.com/no-today/aidev-clis/internal/dbcli"
)

type dataEaseResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type dataEaseQueryData struct {
	Fields []struct {
		FieldName string `json:"fieldName"`
	} `json:"fields"`
	Data []map[string]any `json:"data"`
}

// ParseQueryResponse projects a DataEase sqlPreview envelope into a dbcli.Result.
// Field order defines column order; rows are pulled by column name. A success:false
// envelope whose message looks auth-related maps to DATAEASE_AUTH_EXPIRED so the
// caller can refresh the session; otherwise DATAEASE_QUERY_FAILED.
func ParseQueryResponse(body []byte) (*dbcli.Result, error) {
	var envelope dataEaseResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, errs.Remote("DATAEASE_BAD_RESPONSE", fmt.Sprintf("invalid DataEase response JSON: %v", err))
	}
	if !envelope.Success {
		msg := envelope.Message
		if msg == "" {
			msg = "DataEase query failed"
		}
		if isAuthFailureMessage(msg) {
			return nil, errs.Auth("DATAEASE_AUTH_EXPIRED", msg)
		}
		return nil, errs.Remote("DATAEASE_QUERY_FAILED", msg)
	}
	var data dataEaseQueryData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return nil, errs.Remote("DATAEASE_BAD_RESPONSE", fmt.Sprintf("invalid DataEase query data: %v", err))
	}
	columns := make([]string, 0, len(data.Fields))
	for _, f := range data.Fields {
		columns = append(columns, f.FieldName)
	}
	rows := make([][]any, 0, len(data.Data))
	for _, src := range data.Data {
		row := make([]any, 0, len(columns))
		for _, col := range columns {
			row = append(row, src[col])
		}
		rows = append(rows, row)
	}
	return &dbcli.Result{Columns: columns, Rows: rows}, nil
}

func isAuthFailureMessage(msg string) bool {
	text := strings.ToLower(strings.TrimSpace(msg))
	if text == "" {
		return false
	}
	for _, marker := range []string{
		"token", "unauthorized", "forbidden", "expired", "auth",
		"登录", "登陆", "未登录", "过期", "认证", "鉴权",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
