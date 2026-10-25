package dbscan_test

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/georgysavva/scany/v2/dbscan"
	"github.com/georgysavva/scany/v2/pgxscan"
)

type testModel struct {
	Foo string
	Bar string
}

const (
	multipleRowsQuery = `
		SELECT *
		FROM (
			VALUES ('foo val', 'bar val'), ('foo val 2', 'bar val 2'), ('foo val 3', 'bar val 3')
		) AS t (foo, bar)
	`
	singleRowsQuery = `
		SELECT 'foo val' AS foo, 'bar val' AS bar
	`
	postsWithAuthorsQuery = `
		SELECT p.id AS id, a.id AS "author.id", a.name AS "author.name", a.bio AS "author.bio"
		FROM (
			VALUES ('post 1', 'author 1'), ('post 2', 'author 2'), ('post 3', NULL), ('post 4', 'author 1')
		) AS p (id, author_id)
		LEFT JOIN (
			VALUES ('author 1', 'name 1', 'bio 1'), ('author 2', 'name 2', NULL)
		) AS a (id, name, bio) ON a.id = p.author_id
		ORDER BY p.id
	`
)

func makeStrPtr(v string) *string { return &v }

func makeTimePtr(v time.Time) *time.Time { return &v }

func queryRows(t *testing.T, query string) dbscan.Rows {
	t.Helper()
	pgxRows, err := testDB.Query(ctx, query)
	require.NoError(t, err)
	rows := pgxscan.NewRowsAdapter(pgxRows)
	return rows
}

func getAPI(opts ...dbscan.APIOption) (*dbscan.API, error) {
	if len(opts) < 1 {
		opts = []dbscan.APIOption{}
	}
	opts = append(opts, dbscan.WithScannableTypes(
		(*sql.Scanner)(nil),
	))
	return dbscan.NewAPI(opts...)
}

func scan(t *testing.T, dst interface{}, rows dbscan.Rows) error {
	return scanWithAPI(t, testAPI, dst, rows)
}

func scanWithAPI(t *testing.T, api *dbscan.API, dst interface{}, rows dbscan.Rows) error {
	defer rows.Close() //nolint: errcheck
	rs := api.NewRowScanner(rows)
	rows.Next()
	if err := rs.Scan(dst); err != nil {
		return err
	}
	requireNoRowsErrorsAndClose(t, rows)
	return nil
}

func requireNoRowsErrorsAndClose(t *testing.T, rows dbscan.Rows) {
	t.Helper()
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
}

func allocateDestination(v interface{}) interface{} {
	dstType := reflect.TypeOf(v)
	dst := reflect.New(dstType).Interface()
	return dst
}

func assertDestinationEqual(t *testing.T, expected, dst interface{}) {
	t.Helper()
	got := reflect.ValueOf(dst).Elem().Interface()
	assert.Equal(t, expected, got)
}
