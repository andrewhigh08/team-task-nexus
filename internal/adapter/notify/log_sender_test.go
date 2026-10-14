package notify_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/notify"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

func TestLogSender_LogsTheEvent(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := logger.New(logger.Config{Level: "info", Format: "json", Output: &buf})
	event := testEvent()

	err := notify.NewLogSender(log).Send(t.Context(), event)

	require.NoError(t, err)
	var rec map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &rec))
	assert.Equal(t, "event delivered", rec["msg"])
	assert.Equal(t, "INFO", rec["level"])
	assert.InDelta(t, 42, rec["event_id"], 0)
	assert.Equal(t, "task.assigned", rec["event_type"])
	assert.InDelta(t, 2, rec["attempt"], 0)
	assert.Equal(t, string(event.Payload), rec["payload"])
}
