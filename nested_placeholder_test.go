package squirrel

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

var dollarPlaceholderRe = regexp.MustCompile(`\$(\d+)`)

// assertSequentialDollars checks that $n placeholders appear in order as
// $1..$N, each exactly once, and that N matches the number of args.
func assertSequentialDollars(t *testing.T, sql string, args []interface{}) {
	t.Helper()
	matches := dollarPlaceholderRe.FindAllStringSubmatch(sql, -1)
	for i, m := range matches {
		assert.Equal(t, fmt.Sprint(i+1), m[1], "placeholder %d in %q", i+1, sql)
	}
	assert.Equal(t, len(matches), len(args), "placeholder count vs args in %q", sql)
}

func TestNestedPlaceholdersDollar(t *testing.T) {
	psql := StatementBuilder.PlaceholderFormat(Dollar)
	sub := psql.Select("id").From("t").Where(Eq{"a": 1})

	tests := []struct {
		name     string
		query    Sqlizer
		wantSql  string
		wantArgs []interface{}
	}{
		{
			name:     "Expr with a subquery in WHERE",
			query:    psql.Select("*").From("u").Where(Eq{"b": 0}).Where(Expr("id IN (?)", sub)),
			wantSql:  "SELECT * FROM u WHERE b = $1 AND id IN (SELECT id FROM t WHERE a = $2)",
			wantArgs: []interface{}{0, 1},
		},
		{
			name:     "Expr with a subquery in DELETE WHERE",
			query:    psql.Delete("u").Where(Eq{"b": 0}).Where(Expr("id IN (?)", sub)),
			wantSql:  "DELETE FROM u WHERE b = $1 AND id IN (SELECT id FROM t WHERE a = $2)",
			wantArgs: []interface{}{0, 1},
		},
		{
			name:     "UPDATE SET to a subquery",
			query:    psql.Update("u").Set("b", 0).Set("c", sub).Where(Eq{"d": 9}),
			wantSql:  "UPDATE u SET b = $1, c = (SELECT id FROM t WHERE a = $2) WHERE d = $3",
			wantArgs: []interface{}{0, 1, 9},
		},
		{
			name:     "UPDATE SET to an Expr wrapping a subquery",
			query:    psql.Update("u").Set("b", 0).Set("c", Expr("COALESCE((?), ?)", sub, 5)).Where(Eq{"d": 9}),
			wantSql:  "UPDATE u SET b = $1, c = COALESCE((SELECT id FROM t WHERE a = $2), $3) WHERE d = $4",
			wantArgs: []interface{}{0, 1, 5, 9},
		},
		{
			name:     "INSERT VALUES with a subquery",
			query:    psql.Insert("u").Columns("a", "b").Values(0, Expr("(?)", sub)),
			wantSql:  "INSERT INTO u (a,b) VALUES ($1,(SELECT id FROM t WHERE a = $2))",
			wantArgs: []interface{}{0, 1},
		},
		{
			name:     "INSERT ... SELECT with a suffix arg",
			query:    psql.Insert("u").Columns("a").Select(sub).Suffix("RETURNING ?", 7),
			wantSql:  "INSERT INTO u (a) SELECT id FROM t WHERE a = $1 RETURNING $2",
			wantArgs: []interface{}{1, 7},
		},
		{
			name:     "Alias column",
			query:    psql.Select("x").Column(Alias(sub, "s")).From("u").Where(Eq{"b": 0}),
			wantSql:  "SELECT x, (SELECT id FROM t WHERE a = $1) AS s FROM u WHERE b = $2",
			wantArgs: []interface{}{1, 0},
		},
		{
			name:     "Expr column",
			query:    psql.Select("x").Column(Expr("(?) AS s", sub)).From("u").Where(Eq{"b": 0}),
			wantSql:  "SELECT x, (SELECT id FROM t WHERE a = $1) AS s FROM u WHERE b = $2",
			wantArgs: []interface{}{1, 0},
		},
		{
			name:     "ConcatExpr column",
			query:    psql.Select("x").Column(ConcatExpr("COALESCE((", sub, "), ", Expr("?", 3), ")")).From("u").Where(Eq{"b": 0}),
			wantSql:  "SELECT x, COALESCE((SELECT id FROM t WHERE a = $1), $2) FROM u WHERE b = $3",
			wantArgs: []interface{}{1, 3, 0},
		},
		{
			name:     "Nested Expr inside And/Or",
			query:    psql.Select("*").From("u").Where(Or{Eq{"b": 0}, Expr("id IN (?)", sub)}).Where(Eq{"c": 2}),
			wantSql:  "SELECT * FROM u WHERE (b = $1 OR id IN (SELECT id FROM t WHERE a = $2)) AND c = $3",
			wantArgs: []interface{}{0, 1, 2},
		},
		{
			name:     "CTE with UPDATE ... RETURNING",
			query:    psql.Select("*").With("w", psql.Update("t").Set("x", 1).Where(Eq{"y": 2}).Suffix("RETURNING id")).From("w").Where(Eq{"b": 0}),
			wantSql:  "WITH w AS (UPDATE t SET x = $1 WHERE y = $2 RETURNING id) SELECT * FROM w WHERE b = $3",
			wantArgs: []interface{}{1, 2, 0},
		},
		{
			name:     "CTE with INSERT ... RETURNING",
			query:    psql.Select("*").With("w", psql.Insert("t").Columns("a").Values(1).Suffix("RETURNING id")).From("w").Where(Eq{"b": 0}),
			wantSql:  "WITH w AS (INSERT INTO t (a) VALUES ($1) RETURNING id) SELECT * FROM w WHERE b = $2",
			wantArgs: []interface{}{1, 0},
		},
		{
			name:     "CTE with DELETE ... RETURNING",
			query:    psql.Select("*").With("w", psql.Delete("t").Where(Eq{"a": 1}).Suffix("RETURNING id")).From("w").Where(Eq{"b": 0}),
			wantSql:  "WITH w AS (DELETE FROM t WHERE a = $1 RETURNING id) SELECT * FROM w WHERE b = $2",
			wantArgs: []interface{}{1, 0},
		},
		{
			name: "CTE chain mixing SELECT and UPDATE",
			query: psql.Select("*").
				With("s", sub).
				With("w", psql.Update("t").Set("x", Expr("x + ?", 10)).Where(Expr("id IN (SELECT id FROM s)")).Suffix("RETURNING id")).
				From("w").Where(Eq{"b": 0}),
			wantSql:  "WITH s AS (SELECT id FROM t WHERE a = $1), w AS (UPDATE t SET x = x + $2 WHERE id IN (SELECT id FROM s) RETURNING id) SELECT * FROM w WHERE b = $3",
			wantArgs: []interface{}{1, 10, 0},
		},
		// Compositions that already numbered correctly; kept as regression guards.
		{
			name:     "UPDATE ... FROM subquery",
			query:    psql.Update("u").Set("b", 0).FromSelect(sub, "s").Where(Eq{"d": 9}),
			wantSql:  "UPDATE u SET b = $1 FROM (SELECT id FROM t WHERE a = $2) AS s WHERE d = $3",
			wantArgs: []interface{}{0, 1, 9},
		},
		{
			name:     "UNION",
			query:    psql.Select("a").From("u").Where(Eq{"b": 0}).UnionSelect(psql.Select("a").From("v").Where(Eq{"c": 1})),
			wantSql:  "SELECT a FROM u WHERE b = $1 UNION SELECT a FROM v WHERE c = $2",
			wantArgs: []interface{}{0, 1},
		},
		{
			name:     "JoinClause with a subquery",
			query:    psql.Select("*").From("u").JoinClause(sub.Prefix("JOIN (").Suffix(") s ON s.id = u.id")).Where(Eq{"b": 0}),
			wantSql:  "SELECT * FROM u JOIN ( SELECT id FROM t WHERE a = $1 ) s ON s.id = u.id WHERE b = $2",
			wantArgs: []interface{}{1, 0},
		},
		{
			name:     "CASE column",
			query:    psql.Select("x").Column(Case().When(Expr("a = ?", 1), "1").Else(Expr("?", 2))).From("u").Where(Eq{"b": 0}),
			wantSql:  "SELECT x, CASE WHEN a = $1 THEN 1 ELSE $2 END FROM u WHERE b = $3",
			wantArgs: []interface{}{1, 2, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := tt.query.ToSql()
			assert.NoError(t, err)
			assert.Equal(t, tt.wantSql, sql)
			assert.Equal(t, tt.wantArgs, args)
			assertSequentialDollars(t, sql, args)
		})
	}
}

func TestNestedPlaceholdersOtherFormats(t *testing.T) {
	sub := Select("id").From("t").Where(Eq{"a": 1}).PlaceholderFormat(Colon)
	query := Update("u").Set("b", 0).Set("c", sub).Where(Expr("d IN (?)", sub)).PlaceholderFormat(Colon)

	sql, args, err := query.ToSql()
	assert.NoError(t, err)
	assert.Equal(t, "UPDATE u SET b = :1, c = (SELECT id FROM t WHERE a = :2) WHERE d IN (SELECT id FROM t WHERE a = :3)", sql)
	assert.Equal(t, []interface{}{0, 1, 1}, args)

	sql, _, err = query.PlaceholderFormat(AtP).ToSql()
	assert.NoError(t, err)
	assert.Equal(t, "UPDATE u SET b = @p1, c = (SELECT id FROM t WHERE a = @p2) WHERE d IN (SELECT id FROM t WHERE a = @p3)", sql)
}

func TestNestedPlaceholdersKeepEscapedQuestionMarks(t *testing.T) {
	// "??" is an escaped literal question mark (e.g. the jsonb ? operator).
	// It must survive nesting and be unescaped exactly once by the outer builder.
	psql := StatementBuilder.PlaceholderFormat(Dollar)
	sub := psql.Select("id").From("t").Where("tags ?? ?", "vip")

	sql, args, err := psql.Delete("u").Where(Eq{"b": 0}).Where(Expr("id IN (?)", sub)).ToSql()
	assert.NoError(t, err)
	assert.Equal(t, "DELETE FROM u WHERE b = $1 AND id IN (SELECT id FROM t WHERE tags ? $2)", sql)
	assert.Equal(t, []interface{}{0, "vip"}, args)
}

func TestStandaloneBuildersStillFinalizePlaceholders(t *testing.T) {
	psql := StatementBuilder.PlaceholderFormat(Dollar)

	sql, _, err := psql.Update("t").Set("a", 1).Where(Eq{"b": 2}).ToSql()
	assert.NoError(t, err)
	assert.Equal(t, "UPDATE t SET a = $1 WHERE b = $2", sql)

	sql, _, err = psql.Insert("t").Columns("a", "b").Values(1, 2).ToSql()
	assert.NoError(t, err)
	assert.Equal(t, "INSERT INTO t (a,b) VALUES ($1,$2)", sql)

	sql, _, err = psql.Delete("t").Where(Eq{"a": 1}).ToSql()
	assert.NoError(t, err)
	assert.Equal(t, "DELETE FROM t WHERE a = $1", sql)

	// Calling ToSql on a bare Expr keeps its previous output.
	sql, args, err := Expr("id IN (?)", psql.Select("id").From("t").Where(Eq{"a": 1})).ToSql()
	assert.NoError(t, err)
	assert.Equal(t, "id IN (SELECT id FROM t WHERE a = $1)", sql)
	assert.Equal(t, []interface{}{1}, args)
}
