package migrations_test

import (
	"errors"
	"io"
	"io/fs"
	"testing"

	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/migrations"
)

// TestEmbeddedMigrations walks the embedded source the way golang-migrate does: versions are
// consecutive from 1 and every version has a non-empty up and down script.
func TestEmbeddedMigrations(t *testing.T) {
	t.Parallel()
	src, err := iofs.New(migrations.FS, ".")
	require.NoError(t, err)
	t.Cleanup(func() { _ = src.Close() })

	version, err := src.First()
	require.NoError(t, err)
	require.Equal(t, uint(1), version)

	for {
		for name, read := range map[string]func(uint) (io.ReadCloser, string, error){"up": src.ReadUp, "down": src.ReadDown} {
			r, _, err := read(version)
			require.NoError(t, err, "version %d has no %s script", version, name)
			body, err := io.ReadAll(r)
			require.NoError(t, r.Close())
			require.NoError(t, err)
			assert.NotEmpty(t, body, "version %d %s script is empty", version, name)
		}

		next, err := src.Next(version)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		require.NoError(t, err)
		require.Equal(t, version+1, next, "migration versions must be consecutive")
		version = next
	}
}
