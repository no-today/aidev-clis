package dataease

import (
	"reflect"
	"testing"
)

func TestParseQueryResponse_UsesFieldOrderForRows(t *testing.T) {
	body := []byte(`{
		"success": true,
		"data": {
			"fields": [{"fieldName": "b"}, {"fieldName": "a"}],
			"data": [
				{"a": "first-a", "b": "first-b"},
				{"a": "second-a", "b": "second-b"}
			],
			"log": {"spend": 12}
		}
	}`)

	res, err := ParseQueryResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Columns, []string{"b", "a"}) {
		t.Errorf("Columns = %v", res.Columns)
	}
	want := [][]any{{"first-b", "first-a"}, {"second-b", "second-a"}}
	if !reflect.DeepEqual(res.Rows, want) {
		t.Errorf("Rows = %v, want %v", res.Rows, want)
	}
}

func TestParseQueryResponse_TokenExpiredIsAuthError(t *testing.T) {
	_, err := ParseQueryResponse([]byte(`{"success":false,"message":"token expired"}`))
	requireCode(t, err, "DATAEASE_AUTH_EXPIRED")
}

func TestParseQueryResponse_GenericFailureIsRemoteError(t *testing.T) {
	_, err := ParseQueryResponse([]byte(`{"success":false,"message":"sql denied"}`))
	requireCode(t, err, "DATAEASE_QUERY_FAILED")
}
