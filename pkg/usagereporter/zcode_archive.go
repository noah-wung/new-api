package usagereporter

import (
	"bytes"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"

	"github.com/QuantumNous/new-api/common"
)

// The active state remains bounded; acknowledged snapshots are moved to this
// durable local index so source revisions and duplicate suppression survive.
type zcodeArchive struct{ db *sql.DB }

func openZCodeArchive(statePath, namespace string, create, readOnly bool) (*zcodeArchive, error) {
	path := statePath + ".archive.sqlite"
	info, err := os.Lstat(path)
	created := false
	if os.IsNotExist(err) {
		if !create {
			return nil, nil
		}
		file, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if e != nil {
			return nil, e
		}
		if e = file.Close(); e != nil {
			_ = os.Remove(path)
			return nil, e
		}
		created = true
	} else if err != nil {
		return nil, err
	} else if !info.Mode().IsRegular() {
		return nil, errors.New("invalid ZCode archive file")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		if created {
			_ = os.Remove(path)
		}
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
	query := u.Query()
	if readOnly {
		query.Set("mode", "ro")
		query.Add("_pragma", "query_only(1)")
	} else {
		query.Set("mode", "rw")
	}
	query.Add("_pragma", "busy_timeout(2000)")
	u.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		if created {
			_ = os.Remove(path)
		}
		return nil, errors.New("could not open ZCode archive")
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*zcodeArchive, error) {
		_ = db.Close()
		if created {
			_ = os.Remove(path)
		}
		return nil, e
	}
	if created {
		e := func() error {
			tx, e := db.Begin()
			if e != nil {
				return e
			}
			defer tx.Rollback()
			if _, e = tx.Exec(`CREATE TABLE metadata (version INTEGER NOT NULL, namespace TEXT NOT NULL); CREATE TABLE snapshots (event_id TEXT PRIMARY KEY, snapshot BLOB NOT NULL)`); e != nil {
				return e
			}
			if _, e = tx.Exec(`INSERT INTO metadata (version, namespace) VALUES (1, ?)`, namespace); e != nil {
				return e
			}
			return tx.Commit()
		}()
		if e != nil {
			return fail(errors.New("could not initialize ZCode archive"))
		}
		if e = syncZCodeArchiveDirectory(filepath.Dir(path)); e != nil {
			return fail(errors.New("could not sync ZCode archive directory"))
		}
	}
	var version int
	var stored string
	if err := db.QueryRow(`SELECT version, namespace FROM metadata`).Scan(&version, &stored); err != nil || version != 1 || stored != namespace {
		return fail(errors.New("invalid ZCode archive or identity"))
	}
	var probe int
	if err := db.QueryRow(`SELECT 1 FROM snapshots LIMIT 1`).Scan(&probe); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fail(errors.New("invalid ZCode archive schema"))
	}
	return &zcodeArchive{db: db}, nil
}

func (a *zcodeArchive) get(id string) (zcodeSnapshot, bool, error) {
	var result zcodeSnapshot
	if a == nil {
		return result, false, nil
	}
	var data []byte
	err := a.db.QueryRow(`SELECT snapshot FROM snapshots WHERE event_id = ?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return result, false, nil
	}
	if err != nil || common.Unmarshal(data, &result) != nil || result.Event.EventID != id {
		return result, false, errors.New("invalid ZCode archived snapshot")
	}
	return result, true, nil
}

func (a *zcodeArchive) count() (int64, error) {
	if a == nil {
		return 0, nil
	}
	var count int64
	if err := a.db.QueryRow(`SELECT count(*) FROM snapshots`).Scan(&count); err != nil {
		return 0, errors.New("invalid ZCode archive contents")
	}
	return count, nil
}

func (a *zcodeArchive) store(snapshots map[string]zcodeSnapshot) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id, snapshot := range snapshots {
		data, e := common.Marshal(snapshot)
		if e != nil {
			return e
		}
		result, e := tx.Exec(`INSERT OR IGNORE INTO snapshots (event_id, snapshot) VALUES (?, ?)`, id, data)
		if e != nil {
			return e
		}
		inserted, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if inserted == 0 {
			var old []byte
			if e = tx.QueryRow(`SELECT snapshot FROM snapshots WHERE event_id = ?`, id).Scan(&old); e != nil || !bytes.Equal(old, data) {
				return errors.New("conflicting ZCode archived snapshot")
			}
		}
	}
	return tx.Commit()
}

func (a *zcodeArchive) close() error {
	if a == nil {
		return nil
	}
	return a.db.Close()
}
