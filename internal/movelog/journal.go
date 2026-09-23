package movelog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Journal is one match's durable history.
//
// The chain lives in memory while a match is played, and a process that dies
// mid-match comes back knowing nothing: the SDK restores who is at the table
// and where the money is, but not a single move. Without this the seats cannot
// carry on, cannot settle, and both stakes wait out their refund locks.
//
// It is append-only and fsynced per record, like the signing book beside it,
// and for the same reason: what it protects is only worth anything if it
// survives the crash it exists for.
//
// Two record kinds carry moves, and the order matters. A reservation is written
// before the key signs, and holds the exact unsigned entry; the signed entry is
// written after. A crash between them leaves a reservation whose digest the
// book has already recorded - so the move must be re-signed from those same
// bytes, never chosen afresh, or the book will refuse the position and the seat
// can never move again.
type Journal struct {
	mu   sync.Mutex
	path string
	f    *os.File
}

type journalRecord struct {
	Kind    string          `json:"k"`
	Entry   json.RawMessage `json:"entry,omitempty"`
	Abandon json.RawMessage `json:"abandon,omitempty"`
	Note    string          `json:"note,omitempty"`
}

// Record kinds.
const (
	kindReserve = "reserve"
	kindEntry   = "entry"
	kindAbandon = "abandon"
	kindNote    = "note"
)

// OpenJournal opens or creates a match's journal.
func OpenJournal(path string) (*Journal, error) {
	if path == "" {
		return nil, fmt.Errorf("a journal needs a path")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("journal directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open journal: %w", err)
	}
	if err := syncDir(dir); err != nil {
		f.Close()
		return nil, err
	}
	return &Journal{path: path, f: f}, nil
}

func (j *Journal) append(rec journalRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode journal record: %w", err)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.f == nil {
		return fmt.Errorf("journal %s is closed", j.path)
	}
	if _, err := j.f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append to journal: %w", err)
	}
	if err := j.f.Sync(); err != nil {
		return fmt.Errorf("flush journal to disk: %w", err)
	}
	return nil
}

// Reserve records the exact entry this seat is about to sign, before it signs.
func (j *Journal) Reserve(e *Entry) error {
	body, err := encodeUnsigned(e)
	if err != nil {
		return err
	}
	return j.append(journalRecord{Kind: kindReserve, Entry: body})
}

// Commit records a signed entry, this seat's or the other's.
func (j *Journal) Commit(e *Entry) error {
	body, err := EncodeEntry(e)
	if err != nil {
		return err
	}
	return j.append(journalRecord{Kind: kindEntry, Entry: body})
}

// CommitAbandon records that the match was given up on.
func (j *Journal) CommitAbandon(a *Abandon) error {
	body, err := a.MarshalJSON()
	if err != nil {
		return err
	}
	return j.append(journalRecord{Kind: kindAbandon, Abandon: body})
}

// Note records a fact worth surviving a restart, such as a settlement having
// been proposed.
func (j *Journal) Note(note string) error {
	return j.append(journalRecord{Kind: kindNote, Note: note})
}

// Replayed is what a journal held.
type Replayed struct {
	Chain *Chain
	// Pending is an entry that was reserved and never committed: this seat
	// was interrupted between deciding a move and recording the signed one.
	// It must be re-signed from these exact bytes.
	Pending *Entry
	Abandon *Abandon
	Notes   []string
}

// Replay rebuilds a match from its journal, verifying every entry against the
// roster the escrow committed to. Nothing here is trusted because it was on
// disk: a transcript is only as good as the signatures in it.
func (j *Journal) Replay(matchID string, roster Roster) (*Replayed, error) {
	data, err := os.ReadFile(j.path)
	if errors.Is(err, os.ErrNotExist) {
		data = nil
	} else if err != nil {
		return nil, fmt.Errorf("read journal: %w", err)
	}
	chain, err := NewChain(matchID, roster)
	if err != nil {
		return nil, err
	}
	out := &Replayed{Chain: chain}

	lines := bytes.Split(data, []byte("\n"))
	var good int64
	for i, line := range lines {
		last := i == len(lines)-1
		if len(line) == 0 {
			if !last {
				return nil, fmt.Errorf("journal %s: empty line %d", j.path, i+1)
			}
			break
		}
		var rec journalRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			if last {
				break // a torn tail from the crash this exists for
			}
			return nil, fmt.Errorf("journal %s: line %d is not a record: %w", j.path, i+1, err)
		}
		if err := out.apply(rec); err != nil {
			if last {
				break
			}
			return nil, fmt.Errorf("journal %s: line %d: %w", j.path, i+1, err)
		}
		good += int64(len(line)) + 1
	}
	if good < int64(len(data)) {
		if err := os.Truncate(j.path, good); err != nil {
			return nil, fmt.Errorf("journal %s: drop the torn last line: %w", j.path, err)
		}
	}
	return out, nil
}

func (r *Replayed) apply(rec journalRecord) error {
	switch rec.Kind {
	case kindReserve:
		e, err := decodeUnsigned(rec.Entry)
		if err != nil {
			return err
		}
		r.Pending = e
	case kindEntry:
		e, err := DecodeEntry(rec.Entry)
		if err != nil {
			return err
		}
		if err := r.Chain.Append(e); err != nil {
			return err
		}
		// A committed entry answers the reservation that preceded it.
		r.Pending = nil
	case kindAbandon:
		a, err := DecodeAbandon(rec.Abandon)
		if err != nil {
			return err
		}
		r.Abandon = a
	case kindNote:
		r.Notes = append(r.Notes, rec.Note)
	default:
		return fmt.Errorf("unknown journal record %q", rec.Kind)
	}
	return nil
}

// Close releases the file.
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.f == nil {
		return nil
	}
	err := j.f.Close()
	j.f = nil
	return err
}

// encodeUnsigned renders an entry without its signature, which is exactly what
// a reservation is: the bytes the signature will cover.
func encodeUnsigned(e *Entry) ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("no entry")
	}
	dup := *e
	dup.Sig = make([]byte, SigLen)
	return EncodeEntry(&dup)
}

func decodeUnsigned(blob []byte) (*Entry, error) {
	e, err := DecodeEntry(blob)
	if err != nil {
		return nil, err
	}
	e.Sig = nil
	return e, nil
}
