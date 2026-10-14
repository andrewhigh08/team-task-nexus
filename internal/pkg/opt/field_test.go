package opt_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/pkg/opt"
)

// patch is a partial update body as a handler would declare it.
type patch struct {
	Title    opt.Field[string] `json:"title"`
	Assignee opt.Field[int64]  `json:"assignee_id"`
}

func decodeStrict(body string, dst any) error {
	dec := json.NewDecoder(bytes.NewBufferString(body))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func TestField_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		body     string
		title    opt.Field[string]
		assignee opt.Field[int64]
	}{
		{name: "absent fields keep the zero value", body: `{}`},
		{name: "null is set and null", body: `{"assignee_id":null}`, assignee: opt.Null[int64]()},
		{name: "a value is set", body: `{"title":"Ship it","assignee_id":7}`, title: opt.Of("Ship it"), assignee: opt.Of[int64](7)},
		{name: "the zero value is still set", body: `{"title":"","assignee_id":0}`, title: opt.Of(""), assignee: opt.Of[int64](0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got patch

			require.NoError(t, decodeStrict(tt.body, &got))

			assert.Equal(t, tt.title, got.Title)
			assert.Equal(t, tt.assignee, got.Assignee)
		})
	}
}

func TestField_UnmarshalJSON_Rejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "value of the wrong type", body: `{"assignee_id":"7"}`},
		{name: "malformed value", body: `{"title":tru}`},
		{name: "unknown field next to optional ones", body: `{"title":"x","status":"done"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got patch
			require.Error(t, decodeStrict(tt.body, &got))
		})
	}
}

func TestField_UnmarshalJSON_ReplacesAnEarlierValue(t *testing.T) {
	t.Parallel()
	f := opt.Of("old")

	require.NoError(t, json.Unmarshal([]byte(`null`), &f))

	assert.Equal(t, opt.Null[string](), f)
}

func TestField_Ptr(t *testing.T) {
	t.Parallel()
	assert.Nil(t, opt.Field[int64]{}.Ptr(), "absent")
	assert.Nil(t, opt.Null[int64]().Ptr(), "null")

	f := opt.Of[int64](7)
	p := f.Ptr()
	require.NotNil(t, p)
	assert.Equal(t, int64(7), *p)
	*p = 8
	assert.Equal(t, int64(7), f.Value, "the pointer refers to a copy")
}
