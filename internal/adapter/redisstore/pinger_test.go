package redisstore_test

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/redisstore"
)

func TestPinger_Ping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		serverErr string
	}{
		{name: "redis answers"},
		{name: "redis fails", serverErr: "LOADING Redis is loading the dataset in memory"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mr := miniredis.RunT(t)
			mr.SetError(tt.serverErr)
			client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
			t.Cleanup(func() { _ = client.Close() })

			err := redisstore.NewPinger(client).Ping(t.Context())

			if tt.serverErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tt.serverErr)
		})
	}
}
