package workload

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

type loadSink struct {
	conn     *session
	hint     string
	retry    int
	interval time.Duration
	buf      bytes.Buffer
	rows     int
}

func newLoadSink(conn *session, hint string, retry int, interval time.Duration) *loadSink {
	return &loadSink{conn: conn, hint: hint, retry: retry, interval: interval}
}
func (s *loadSink) WriteRow(ctx context.Context, values ...any) error {
	if s.rows == 0 {
		s.buf.WriteString(s.hint)
	} else {
		s.buf.WriteString(", ")
	}
	s.buf.WriteByte('(')
	for i, v := range values {
		if i > 0 {
			s.buf.WriteByte(',')
		}
		if v == nil {
			s.buf.WriteString("NULL")
			continue
		}
		switch v := v.(type) {
		case sql.NullString:
			if v.Valid {
				fmt.Fprintf(&s.buf, "'%s'", v.String)
			} else {
				s.buf.WriteString("NULL")
			}
			continue
		case sql.NullInt64:
			if v.Valid {
				fmt.Fprint(&s.buf, v.Int64)
			} else {
				s.buf.WriteString("NULL")
			}
			continue
		}
		switch reflect.TypeOf(v).Kind() {
		case reflect.String:
			fmt.Fprintf(&s.buf, "'%s'", v)
		case reflect.Float32, reflect.Float64:
			fmt.Fprintf(&s.buf, "%f", v)
		default:
			fmt.Fprint(&s.buf, v)
		}
	}
	s.buf.WriteByte(')')
	s.rows++
	if s.rows >= 1024 {
		return s.Flush(ctx)
	}
	return nil
}
func (s *loadSink) Flush(ctx context.Context) error {
	if s.rows == 0 {
		return nil
	}
	var err error
	for attempt := 0; attempt <= s.retry; attempt++ {
		_, err = s.conn.ExecContext(ctx, s.buf.String())
		if err == nil {
			break
		}
		if strings.Contains(err.Error(), "Error 1062: Duplicate entry") {
			if attempt == 0 {
				return err
			}
			break
		}
		if attempt < s.retry {
			if pause(ctx, s.interval) != nil {
				return ctx.Err()
			}
		}
	}
	s.rows = 0
	s.buf.Reset()
	// Failed final batches must not masquerade as a successful SDK load.
	if err != nil {
		return errors.New("go-tpc load batch: " + err.Error())
	}
	return nil
}
