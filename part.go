package squirrel

import (
	"fmt"
	"io"
)

type part struct {
	pred interface{}
	args []interface{}
}

func newPart(pred interface{}, args ...interface{}) Sqlizer {
	return &part{pred, args}
}

func (p part) ToSql() (sql string, args []interface{}, err error) {
	switch pred := p.pred.(type) {
	case nil:
		// no-op
	case Sqlizer:
		sql, args, err = nestedToSql(pred)
	case string:
		sql = pred
		args = p.args
	default:
		err = fmt.Errorf("expected string or Sqlizer, not %T", pred)
	}
	return
}

// finalToSql calls ToSql, finalizing placeholders in the Sqlizer's own format.
func finalToSql(s Sqlizer) (string, []interface{}, error) {
	return s.ToSql()
}

// nestedToSql builds a Sqlizer that is being embedded in another statement.
// Builders and expressions that implement rawSqlizer are built without
// replacing placeholders, so the enclosing statement can number them.
func nestedToSql(s Sqlizer) (string, []interface{}, error) {
	if raw, ok := s.(rawSqlizer); ok {
		return raw.toSqlRaw()
	} else {
		return s.ToSql()
	}
}

func appendToSql(parts []Sqlizer, w io.Writer, sep string, args []interface{}) ([]interface{}, error) {
	for i, p := range parts {
		partSql, partArgs, err := nestedToSql(p)
		if err != nil {
			return nil, err
		} else if len(partSql) == 0 {
			continue
		}

		if i > 0 {
			_, err := io.WriteString(w, sep)
			if err != nil {
				return nil, err
			}
		}

		_, err = io.WriteString(w, partSql)
		if err != nil {
			return nil, err
		}
		args = append(args, partArgs...)
	}
	return args, nil
}
