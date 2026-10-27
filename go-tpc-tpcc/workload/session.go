package workload

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stroppy-io/stroppy/v6/pkg/bench"
)

type session struct {
	handle *bench.Tx
	driver string
}

type statement struct {
	conn *session
	text string
}

var templates sync.Map

type template struct {
	text  string
	names []string
}

func bind(query string, args []any) (string, map[string]any, error) {
	if len(args) == 0 {
		return query, nil, nil
	}
	cached, ok := templates.Load(query)
	if !ok {
		var b strings.Builder
		var names []string
		for _, c := range query {
			if c == '?' {
				name := fmt.Sprintf("p%d", len(names))
				names = append(names, name)
				b.WriteString(" :" + name + " ")
			} else {
				b.WriteRune(c)
			}
		}
		cached, _ = templates.LoadOrStore(query, template{b.String(), names})
	}
	t := cached.(template)
	if len(t.names) != len(args) {
		return "", nil, fmt.Errorf("SQL expects %d arguments, got %d", len(t.names), len(args))
	}
	values := make(map[string]any, len(args))
	for i, name := range t.names {
		values[name] = args[i]
	}
	return t.text, values, nil
}

func (s *session) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	text, values, err := bind(query, args)
	if err != nil {
		return nil, err
	}
	return nil, s.handle.Exec(ctx, text, values)
}
func (s *session) QueryContext(ctx context.Context, query string, args ...any) (*cursor, error) {
	text, values, err := bind(query, args)
	if err != nil {
		return nil, err
	}
	rows, err := s.handle.Query(ctx, text, values)
	if err != nil {
		return nil, err
	}
	return &cursor{rows: rows}, nil
}
func (s *session) QueryRowContext(ctx context.Context, query string, args ...any) *singleRow {
	rows, err := s.QueryContext(ctx, query, args...)
	return &singleRow{rows: rows, err: err}
}
func prepareStmt(_ string, _ context.Context, s *session, query string) *statement {
	return &statement{conn: s, text: query}
}
func (s *statement) ExecContext(ctx context.Context, args ...any) (sql.Result, error) {
	return s.conn.ExecContext(ctx, s.text, args...)
}
func (s *statement) QueryContext(ctx context.Context, args ...any) (*cursor, error) {
	return s.conn.QueryContext(ctx, s.text, args...)
}
func (s *statement) QueryRowContext(ctx context.Context, args ...any) *singleRow {
	return s.conn.QueryRowContext(ctx, s.text, args...)
}

type cursor struct{ rows *bench.Rows }

func (c *cursor) Next() bool             { return c.rows.Next() }
func (c *cursor) Err() error             { return c.rows.Err() }
func (c *cursor) Close() error           { return c.rows.Close() }
func (c *cursor) Scan(dest ...any) error { return scan(c.rows.Row().Values(), dest) }

type singleRow struct {
	rows *cursor
	err  error
}

func (r *singleRow) Scan(dest ...any) (err error) {
	if r.err != nil {
		return r.err
	}
	defer func() { err = errors.Join(err, r.rows.Close()) }()
	if !r.rows.Next() {
		if err := r.rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	return r.rows.Scan(dest...)
}

func scan(values []any, dest []any) error {
	if len(values) != len(dest) {
		return fmt.Errorf("row has %d values, Scan has %d destinations", len(values), len(dest))
	}
	for i, v := range values {
		if stamp, ok := v.(time.Time); ok {
			v = stamp.Format(time.RFC3339Nano)
		}
		if scanner, ok := dest[i].(sql.Scanner); ok {
			if err := scanner.Scan(v); err != nil {
				return err
			}
			continue
		}
		if v == nil {
			return fmt.Errorf("NULL column %d", i)
		}
		text := fmt.Sprint(v)
		if bytes, ok := v.([]byte); ok {
			text = string(bytes)
		}
		switch d := dest[i].(type) {
		case *string:
			*d = text
		case *[]byte:
			*d = []byte(text)
		case *int:
			n, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				return err
			}
			*d = int(n)
		case *int64:
			n, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				return err
			}
			*d = n
		case *float64:
			n, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return err
			}
			*d = n
		default:
			return fmt.Errorf("unsupported Scan target %T", d)
		}
	}
	return nil
}

type transaction struct {
	session *session
	ctx     context.Context
	done    bool
}

func (t *transaction) Commit() error {
	if t.done {
		return sql.ErrTxDone
	}
	_, err := t.session.ExecContext(t.ctx, "COMMIT")
	if err == nil {
		t.done = true
	}
	return err
}
func (t *transaction) Rollback() error {
	if t.done {
		return sql.ErrTxDone
	}
	t.done = true
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.ctx), 30*time.Second)
	defer cancel()
	_, err := t.session.ExecContext(ctx, "ROLLBACK")
	return err
}

func (w *Workloader) beginTx(ctx context.Context) (*transaction, error) {
	s := getTPCCState(ctx).Conn
	isolation := w.cfg.Isolation
	statement := "BEGIN"
	levels := map[int]string{1: "READ UNCOMMITTED", 2: "READ COMMITTED", 4: "REPEATABLE READ", 6: "SERIALIZABLE"}
	if isolation != 0 {
		level, ok := levels[isolation]
		if !ok {
			return nil, fmt.Errorf("unsupported isolation %d", isolation)
		}
		if w.cfg.Driver == "mysql" {
			if _, err := s.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL "+level); err != nil {
				return nil, err
			}
		} else {
			statement += " ISOLATION LEVEL " + level
		}
	}
	if _, err := s.ExecContext(ctx, statement); err != nil {
		return nil, err
	}
	return &transaction{session: s, ctx: ctx}, nil
}
