package movelog

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/karamble/dcrgaming-sdk/pkg/forfeit"
)

// Book is the durable record of what a log key has already signed and where.
//
// The SDK ships only an in-process book, which by its own account "catches the
// mistake within one process, and no further". That is not enough here. The
// hazard is not a program signing twice in a loop; it is a program that crashes
// after move seven, starts again, rebuilds its position counter from something
// that forgot, and signs move seven over a different shot. Two signatures at
// one position publish the signing key, so a book that does not outlive the
// process is a key with no memory of what it has already put its name to.
//
// The file is append-only and every record is fsynced before Record returns,
// because the SDK records a position *before* it signs it and treats a failure
// to record as a refusal to sign. That order is what makes a crash cost nothing:
// a position remembered with nothing signed is harmless, since the same digest
// may be signed again. The other order hands back a signature the book never
// heard of.
type Book struct {
	mu   sync.Mutex
	path string
	f    *os.File
	at   map[forfeit.Position][32]byte
}

// record is one remembered position, as a line in the file.
type record struct {
	Match  string `json:"match"`
	Domain string `json:"domain"`
	Seq    uint64 `json:"seq"`
	Digest string `json:"digest"`
}

// OpenBook loads the book at path, creating it if it is not there yet.
func OpenBook(path string) (*Book, error) {
	if path == "" {
		return nil, fmt.Errorf("a book needs a path")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("book directory: %w", err)
	}
	b := &Book{path: path, at: map[forfeit.Position][32]byte{}}
	if err := b.load(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open book: %w", err)
	}
	b.f = f
	// The file's own directory entry has to be durable too, or a crash can
	// lose the whole book rather than its last line.
	if err := syncDir(dir); err != nil {
		f.Close()
		return nil, err
	}
	return b, nil
}

// load replays the file into memory.
//
// A crash can leave a half-written final line. That one is dropped and the file
// truncated back to the last complete record, because a torn tail is the
// expected outcome of the very failure this book exists for. A malformed line
// anywhere else is real corruption and is refused: continuing past it would
// mean signing at positions the book has silently forgotten.
func (b *Book) load() error {
	data, err := os.ReadFile(b.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read book: %w", err)
	}
	var good int64
	lines := bytes.Split(data, []byte("\n"))
	for i, line := range lines {
		last := i == len(lines)-1
		if len(line) == 0 {
			if !last {
				return fmt.Errorf("book %s: empty line %d", b.path, i+1)
			}
			break
		}
		var r record
		if err := json.Unmarshal(line, &r); err != nil {
			if last {
				break // torn tail from a crash mid-append
			}
			return fmt.Errorf("book %s: line %d is not a record: %w", b.path, i+1, err)
		}
		digest, err := hex.DecodeString(r.Digest)
		if err != nil || len(digest) != 32 {
			if last {
				break
			}
			return fmt.Errorf("book %s: line %d has no 32-byte digest", b.path, i+1)
		}
		var d [32]byte
		copy(d[:], digest)
		b.at[forfeit.Position{Match: r.Match, Domain: forfeit.Domain(r.Domain), Seq: r.Seq}] = d
		good += int64(len(line)) + 1
	}
	if good < int64(len(data)) {
		if err := os.Truncate(b.path, good); err != nil {
			return fmt.Errorf("book %s: drop the torn last line: %w", b.path, err)
		}
	}
	return nil
}

// Used reports what was signed at a position, if anything was.
func (b *Book) Used(p forfeit.Position) ([32]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.at[p]
	return d, ok
}

// Record remembers a position durably, and fails rather than forgetting.
//
// A failure here is a refusal to sign, which is the safe direction: the caller
// gets no signature, and the position stays available for the same digest.
func (b *Book) Record(p forfeit.Position, digest [32]byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.f == nil {
		return fmt.Errorf("book %s is closed", b.path)
	}
	if was, ok := b.at[p]; ok && was == digest {
		return nil // already durable
	}
	line, err := json.Marshal(record{
		Match:  p.Match,
		Domain: string(p.Domain),
		Seq:    p.Seq,
		Digest: hex.EncodeToString(digest[:]),
	})
	if err != nil {
		return fmt.Errorf("encode book record: %w", err)
	}
	if _, err := b.f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append to book: %w", err)
	}
	if err := b.f.Sync(); err != nil {
		return fmt.Errorf("flush book to disk: %w", err)
	}
	b.at[p] = digest
	return nil
}

// Close releases the file. A closed book refuses to record, which refuses to
// sign, which is correct: it cannot promise to remember any more.
func (b *Book) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.f == nil {
		return nil
	}
	err := b.f.Close()
	b.f = nil
	return err
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open book directory: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("flush book directory: %w", err)
	}
	return nil
}
