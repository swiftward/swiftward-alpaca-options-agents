// The arena's store: a participant's book outlives a restart of the process.
//
// Without it the instrument would be measuring the proxy's uptime rather than
// the agent: a crashed process would write open positions and standing orders
// down to nothing, and a participant would wake to a clean account in the middle
// of the day. One file per arena; participants are kept apart by a column
// holding the HASH of the token, because the token is an access key and putting
// it in the database in clear text is keeping a password in the log.
//
// The book is written whole, in one transaction, on every change. A book is tens
// of rows; incremental UPDATEs would save microseconds and cost the database
// parting ways with memory in some one forgotten transition - which is exactly
// the kind of mistake that cannot be noticed.
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure Go, no cgo: the arena builds with one command
)

// Store is the SQLite file holding every book in the arena.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS books (
  token_hash  TEXT PRIMARY KEY,
  name        TEXT NOT NULL DEFAULT '',
  cash        REAL NOT NULL,
  start       REAL NOT NULL,
  last_equity REAL NOT NULL,
  closed_on   TEXT NOT NULL DEFAULT '',
  seq         INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS positions (
  token_hash TEXT NOT NULL,
  symbol     TEXT NOT NULL,
  qty        INTEGER NOT NULL,
  avg_price  REAL NOT NULL,
  class      TEXT NOT NULL,
  mark       REAL NOT NULL DEFAULT 0,
  PRIMARY KEY (token_hash, symbol)
);
CREATE TABLE IF NOT EXISTS orders (
  token_hash   TEXT NOT NULL,
  id           TEXT NOT NULL,
  client_id    TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL,
  qty          INTEGER NOT NULL,
  filled_qty   INTEGER NOT NULL,
  limit_price  REAL NOT NULL,
  filled_avg   REAL NOT NULL,
  fees         REAL NOT NULL,
  tif          TEXT NOT NULL,
  market       INTEGER NOT NULL DEFAULT 0,
  legs         TEXT NOT NULL,
  submitted_at TEXT NOT NULL,
  filled_at    TEXT NOT NULL DEFAULT '',
  canceled_at  TEXT NOT NULL DEFAULT '',
  expires_at   TEXT NOT NULL DEFAULT '',
  replaced_by  TEXT NOT NULL DEFAULT '',
  replaces     TEXT NOT NULL DEFAULT '',
  why          TEXT NOT NULL DEFAULT '',
  turn_ref     TEXT NOT NULL DEFAULT '',
  stand        INTEGER NOT NULL DEFAULT 0,
  seq          INTEGER NOT NULL,
  PRIMARY KEY (token_hash, id)
);
CREATE TABLE IF NOT EXISTS events (
  token_hash TEXT NOT NULL,
  n          INTEGER NOT NULL,
  order_id   TEXT NOT NULL DEFAULT '',
  kind       TEXT NOT NULL,
  symbol     TEXT NOT NULL DEFAULT '',
  at         TEXT NOT NULL,
  sets       INTEGER NOT NULL DEFAULT 0,
  price      REAL NOT NULL DEFAULT 0,
  fees       REAL NOT NULL DEFAULT 0,
  filled     INTEGER NOT NULL DEFAULT 0,
  why        TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (token_hash, n)
);
`

func OpenStore(path string) (*Store, error) {
	// WAL and busy_timeout: the matcher writes on its own beat and participants
	// read on theirs, and a "database is locked" in the middle of a fill would be
	// a lost trade rather than a delay.
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// Books created before these columns existed carry on: SQLite has no ADD
	// COLUMN IF NOT EXISTS, so we try and quietly accept that the column is
	// already there. Losing a book over a column is not on.
	defer func() {
		for _, add := range []string{
			`ALTER TABLE books ADD COLUMN name TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE orders ADD COLUMN turn_ref TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE orders ADD COLUMN stand INTEGER NOT NULL DEFAULT 0`,
		} {
			_, _ = db.Exec(add)
		}
	}()

	if _, err := db.Exec(schema); err != nil {
		db.Close()

		return nil, fmt.Errorf("create the tables in %s: %w", path, err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}

	return s.db.Close()
}

// Save writes the book whole. Called under the book's lock: outside the
// transaction, memory and file are required to agree.
func (s *Store) Save(b *Book) error {
	if s == nil {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // a rollback after a successful Commit is not an error

	if _, err := tx.Exec(
		`INSERT INTO books (token_hash, name, cash, start, last_equity, closed_on, seq)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(token_hash) DO UPDATE SET
		   name = CASE WHEN excluded.name = '' THEN books.name ELSE excluded.name END,
		   cash = excluded.cash, start = excluded.start,
		   last_equity = excluded.last_equity, closed_on = excluded.closed_on,
		   seq = excluded.seq`,
		b.Hash, b.Name, b.Cash, b.Start, b.LastEquity, b.ClosedOn, b.seq); err != nil {
		return err
	}

	if _, err := tx.Exec(`DELETE FROM positions WHERE token_hash = ?`, b.Hash); err != nil {
		return err
	}
	for _, p := range b.Positions {
		if _, err := tx.Exec(
			`INSERT INTO positions (token_hash, symbol, qty, avg_price, class, mark)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			b.Hash, p.Symbol, p.Qty, p.AvgPrice, p.Class, p.Mark); err != nil {
			return err
		}
	}

	for _, o := range b.Orders {
		legs, err := json.Marshal(o.Legs)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO orders (token_hash, id, client_id, status, qty, filled_qty,
			   limit_price, filled_avg, fees, tif, market, legs, submitted_at, filled_at,
			   canceled_at, expires_at, replaced_by, replaces, why, turn_ref, stand, seq)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(token_hash, id) DO UPDATE SET
			   client_id = excluded.client_id, status = excluded.status,
			   filled_qty = excluded.filled_qty, limit_price = excluded.limit_price,
			   filled_avg = excluded.filled_avg, fees = excluded.fees,
			   filled_at = excluded.filled_at, canceled_at = excluded.canceled_at,
			   expires_at = excluded.expires_at, replaced_by = excluded.replaced_by,
			   why = excluded.why, turn_ref = excluded.turn_ref,
			   stand = excluded.stand`,
			b.Hash, o.ID, o.ClientID, o.Status, o.Qty, o.FilledQty, o.Limit,
			o.FilledAvg, o.Fees, o.TIF, boolInt(o.Market), string(legs), stamp(o.SubmittedAt),
			stamp(o.FilledAt), stamp(o.CanceledAt), stamp(o.ExpiresAt),
			o.ReplacedBy, o.Replaces, o.Why, o.TurnRef, boolInt(o.Stand), o.Seq); err != nil {
			return err
		}
	}

	// Events are appended, not rewritten: the arena's log only grows.
	for i := b.savedEvents; i < len(b.Events); i++ {
		e := b.Events[i]
		if _, err := tx.Exec(
			`INSERT INTO events (token_hash, n, order_id, kind, symbol, at, sets, price, fees, filled, why)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(token_hash, n) DO NOTHING`,
			b.Hash, i, e.OrderID, e.Kind, e.Symbol, stamp(e.At), e.Sets, e.Price,
			e.Fees, boolInt(e.Filled), e.Why); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	b.savedEvents = len(b.Events)

	return nil
}

// Load raises a participant's book. The second value says whether it was in the
// file at all: no book and a book with no cash are different things.
func (s *Store) Load(b *Book) (bool, error) {
	if s == nil {
		return false, nil
	}

	row := s.db.QueryRow(
		`SELECT name, cash, start, last_equity, closed_on, seq FROM books WHERE token_hash = ?`, b.Hash)
	// The stored name does not overwrite what the roster gave: the roster's name
	// is the fresher one, and the stored one can be empty in books created before
	// this column existed.
	var stored string
	err := row.Scan(&stored, &b.Cash, &b.Start, &b.LastEquity, &b.ClosedOn, &b.seq)
	if b.Name == "" {
		b.Name = stored
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	rows, err := s.db.Query(
		`SELECT symbol, qty, avg_price, class, mark FROM positions WHERE token_hash = ?`, b.Hash)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		p := &Position{}
		if err := rows.Scan(&p.Symbol, &p.Qty, &p.AvgPrice, &p.Class, &p.Mark); err != nil {
			rows.Close()

			return false, err
		}
		b.Positions[p.Symbol] = p
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}

	orders, err := s.db.Query(
		`SELECT id, client_id, status, qty, filled_qty, limit_price, filled_avg, fees,
		        tif, market, legs, submitted_at, filled_at, canceled_at, expires_at,
		        replaced_by, replaces, why, turn_ref, stand, seq
		 FROM orders WHERE token_hash = ? ORDER BY seq`, b.Hash)
	if err != nil {
		return false, err
	}
	defer orders.Close()

	for orders.Next() {
		o := &Order{}
		var legs, submitted, filled, canceled, expires string
		var market, stand int
		if err := orders.Scan(&o.ID, &o.ClientID, &o.Status, &o.Qty, &o.FilledQty,
			&o.Limit, &o.FilledAvg, &o.Fees, &o.TIF, &market, &legs, &submitted, &filled,
			&canceled, &expires, &o.ReplacedBy, &o.Replaces, &o.Why, &o.TurnRef, &stand, &o.Seq); err != nil {
			return false, err
		}
		o.Stand = stand != 0
		if err := json.Unmarshal([]byte(legs), &o.Legs); err != nil {
			return false, fmt.Errorf("the legs of order %s cannot be parsed: %w", o.ID, err)
		}
		o.Market = market != 0
		o.SubmittedAt = unstamp(submitted)
		o.FilledAt = unstamp(filled)
		o.CanceledAt = unstamp(canceled)
		o.ExpiresAt = unstamp(expires)
		b.Orders[o.ID] = o
		b.order = append(b.order, o.ID)
	}
	if err := orders.Err(); err != nil {
		return false, err
	}

	// Events are not raised into memory: the log is for reading afterwards, and
	// the book does not need it to work. The counter is set from what is already
	// in the file, or the very first write would try to put event 0 on top of an
	// existing one.
	var saved int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE token_hash = ?`, b.Hash).Scan(&saved); err != nil {
		return false, err
	}
	b.savedEvents = saved
	b.Events = make([]Event, saved)

	return true, nil
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.UTC().Format(time.RFC3339Nano)
}

func unstamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}

	return t
}

func boolInt(b bool) int {
	if b {
		return 1
	}

	return 0
}
