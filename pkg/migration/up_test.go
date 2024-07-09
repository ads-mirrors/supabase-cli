package migration

import (
	"context"
	"embed"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/supabase/cli/pkg/pgtest"
)

func TestPendingMigrations(t *testing.T) {
	t.Run("finds pending migrations", func(t *testing.T) {
		local := []string{
			"20221201000000_test.sql",
			"20221201000001_test.sql",
			"20221201000002_test.sql",
			"20221201000003_test.sql",
		}
		remote := []string{
			"20221201000000",
			"20221201000001",
		}
		// Run test
		pending, err := FindPendingMigrations(local, remote)
		// Check error
		assert.NoError(t, err)
		assert.ElementsMatch(t, local[2:], pending)
	})

	t.Run("throws error on missing local migration", func(t *testing.T) {
		local := []string{}
		remote := []string{"0"}
		// Run test
		pending, err := FindPendingMigrations(local, remote)
		// Check error
		assert.ErrorIs(t, err, ErrMissingLocal)
		assert.ElementsMatch(t, remote, pending)
	})

	t.Run("throws error on missing remote version", func(t *testing.T) {
		local := []string{
			"0_test.sql",
			"1_test.sql",
		}
		remote := []string{"1"}
		// Run test
		pending, err := FindPendingMigrations(local, remote)
		// Check error
		assert.ErrorIs(t, err, ErrMissingRemote)
		assert.ElementsMatch(t, local[:1], pending)
	})

	t.Run("throws error on out-of-order remote migrations", func(t *testing.T) {
		local := []string{
			"20221201000000_test.sql",
			"20221201000001_test.sql",
			"20221201000002_test.sql",
			"20221201000003_test.sql",
			"20221201000004_test.sql",
		}
		remote := []string{
			"20221201000002",
			"20221201000004",
		}
		// Run test
		missing, err := FindPendingMigrations(local, remote)
		// Check error
		assert.ErrorIs(t, err, ErrMissingRemote)
		assert.ElementsMatch(t, []string{local[0], local[1], local[3]}, missing)
	})

	t.Run("throws error on out-of-order local migrations", func(t *testing.T) {
		local := []string{
			"20221201000000_test.sql",
			"20221201000002_test.sql",
		}
		remote := []string{
			"20221201000000",
			"20221201000001",
			"20221201000002",
			"20221201000003",
			"20221201000004",
		}
		// Run test
		missing, err := FindPendingMigrations(local, remote)
		// Check error
		assert.ErrorIs(t, err, ErrMissingLocal)
		assert.ElementsMatch(t, []string{remote[1], remote[3], remote[4]}, missing)
	})
}

var (
	//go:embed testdata
	testMigrations embed.FS
	//go:embed testdata/0_schema.sql
	testSchema string
)

func TestMigrateUp(t *testing.T) {
	t.Run("applies migrations and appends history", func(t *testing.T) {
		pending := []string{"testdata/0_schema.sql"}
		// Setup mock postgres
		conn := pgtest.NewConn()
		defer conn.Close(t)
		MockMigrationHistory(conn).
			Query(testSchema).
			Reply("CREATE SCHEMA").
			Query(INSERT_MIGRATION_VERSION, "0", "schema", []string{testSchema}).
			Reply("INSERT 0 1")
		// Run test
		err := MigrateUp(context.Background(), conn.MockClient(t), pending, testMigrations)
		// Check error
		assert.NoError(t, err)
	})
}

func MockMigrationHistory(conn *pgtest.MockConn) *pgtest.MockConn {
	conn.Query(SET_LOCK_TIMEOUT).
		Query(CREATE_VERSION_SCHEMA).
		Reply("CREATE SCHEMA").
		Query(CREATE_VERSION_TABLE).
		Reply("CREATE TABLE").
		Query(ADD_STATEMENTS_COLUMN).
		Reply("ALTER TABLE").
		Query(ADD_NAME_COLUMN).
		Reply("ALTER TABLE")
	return conn
}
