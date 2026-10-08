//go:build unix

package smip

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const MaxOutboxRecords = 1000
const (
	Pending   = "pending"
	Uncertain = "uncertain"
	Blocked   = "blocked"
	Accepted  = "accepted"
	Expired   = "expired"
)

var ErrNotDue = errors.New("outbox entry is not due")
var ErrQueueFull = errors.New("outbox record limit reached")
var ErrTerminal = errors.New("outbox entry cannot be resumed")

// Outbound contains the immutable signed packet and mutable delivery state.
// A blocked attempt may already have reached the peer: it is not proof of
// rejection. ReceiptKey records the key trusted when acceptance was verified.
type Outbound struct {
	Packet      Packet   `json:"packet"`
	State       string   `json:"state"`
	Attempts    int      `json:"attempts"`
	NextAttempt int64    `json:"nextAttempt"`
	Updated     int64    `json:"updated"`
	LastStatus  int      `json:"lastStatus"`
	Receipt     *Receipt `json:"receipt,omitempty"`
	ReceiptKey  *Key     `json:"receiptKey,omitempty"`
}
type DueEntry struct {
	ID, State, Destination string
	NextAttempt            int64
}

// FileOutbox is an exclusive, private Unix reference journal. It stores no
// private keys and performs no automatic discovery or transport fallback.
type FileOutbox struct {
	journal *FileInbox
	domain  string
	keys    map[string]Key
	sendMu  sync.Mutex
}

func OpenOutbox(root, domain string, keys map[string]Key) (*FileOutbox, error) {
	if !validDomain(domain) {
		return nil, errors.New("invalid outbox domain")
	}
	cloned, err := cloneKeys(keys)
	if err != nil {
		return nil, err
	}
	if len(cloned) == 0 {
		return nil, errors.New("outbox requires historical origin public keys")
	}
	journal, err := OpenInbox(root)
	if err != nil {
		return nil, err
	}
	return &FileOutbox{journal: journal, domain: domain, keys: cloned}, nil
}
func (s *FileOutbox) Close() error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.journal.Close()
}

// SetKeys fences local key revocation against dispatch already in progress.
func (s *FileOutbox) SetKeys(keys map[string]Key) error {
	cloned, err := cloneKeys(keys)
	if err != nil {
		return err
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	s.journal.mu.Lock()
	defer s.journal.mu.Unlock()
	s.keys = cloned
	return nil
}
func validClock(now int64) bool { return now > 0 && now < 1<<53 }
func (s *FileOutbox) get(id string) (Outbound, bool, error) {
	b, found, err := s.journal.read(recordName(s.domain, id), MaxWire+4096)
	if err != nil || !found {
		return Outbound{}, found, err
	}
	var r Outbound
	if err = strictJSON(b, &r); err != nil {
		return Outbound{}, false, err
	}
	key, known := s.keys[r.Packet.Envelope.KeyID]
	// Historical records remain readable after local revocation. Dispatch and
	// enqueue separately require the currently trusted, unrevoked origin key.
	key.Revoked = false
	if !known || r.Packet.Envelope.From != s.domain || r.Packet.Envelope.ID != id || r.Packet.Verify(key) != nil || r.Attempts < 0 || r.Attempts > 1000000 || !validClock(r.Updated) || r.LastStatus < 0 || r.LastStatus > 599 {
		return Outbound{}, false, errors.New("corrupt outbox record")
	}
	if r.State != Pending && r.State != Uncertain && r.State != Blocked && r.State != Accepted && r.State != Expired {
		return Outbound{}, false, errors.New("invalid outbox state")
	}
	if r.State == Accepted {
		if r.Receipt == nil || r.ReceiptKey == nil || r.Receipt.Verify(r.Packet, *r.ReceiptKey) != nil || r.NextAttempt != 0 || r.Attempts == 0 {
			return Outbound{}, false, errors.New("invalid accepted record")
		}
	} else {
		if r.Receipt != nil || r.ReceiptKey != nil {
			return Outbound{}, false, errors.New("unexpected outbox receipt")
		}
		if r.State == Pending && (r.Attempts != 0 || r.NextAttempt < r.Updated) {
			return Outbound{}, false, errors.New("invalid pending record")
		}
		if r.State == Uncertain && (r.Attempts == 0 || r.NextAttempt < r.Updated) {
			return Outbound{}, false, errors.New("invalid uncertain record")
		}
		if (r.State == Blocked || r.State == Expired) && r.NextAttempt != 0 {
			return Outbound{}, false, errors.New("invalid stopped record")
		}
		if r.State == Expired && r.Attempts != 0 {
			return Outbound{}, false, errors.New("attempted packet cannot be certainly expired")
		}
	}
	return r, true, nil
}
func (s *FileOutbox) save(r Outbound) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return s.journal.write(recordName(s.domain, r.Packet.Envelope.ID), b)
}
func (s *FileOutbox) Get(id string) (Outbound, bool, error) {
	s.journal.mu.Lock()
	defer s.journal.mu.Unlock()
	return s.get(id)
}
func (s *FileOutbox) Enqueue(p Packet, now time.Time) (Outbound, error) {
	s.journal.mu.Lock()
	defer s.journal.mu.Unlock()
	key, known := s.keys[p.Envelope.KeyID]
	if !known || p.Envelope.From != s.domain || p.Verify(key) != nil {
		return Outbound{}, errors.New("untrusted outgoing packet")
	}
	old, found, err := s.get(p.Envelope.ID)
	if err != nil {
		return Outbound{}, err
	}
	if found {
		if old.Packet.Digest() != p.Digest() {
			return Outbound{}, ErrConflict
		}
		return old, nil
	}
	clock := now.Unix()
	if !validClock(clock) || p.Envelope.Created > clock+120 || p.Envelope.Expires <= clock {
		return Outbound{}, errors.New("outgoing packet expired or future-dated")
	}
	entries, err := s.entries()
	if err != nil {
		return Outbound{}, err
	}
	if len(entries) >= MaxOutboxRecords {
		return Outbound{}, ErrQueueFull
	}
	r := Outbound{Packet: p, State: Pending, NextAttempt: clock, Updated: clock}
	if err = s.save(r); err != nil {
		return Outbound{}, err
	}
	return r, nil
}
func (s *FileOutbox) entries() ([]string, error) {
	if s.journal.closed {
		return nil, errors.New("outbox closed")
	}
	entries, err := os.ReadDir(s.journal.root)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			names = append(names, entry.Name())
		}
	}
	if len(names) > MaxOutboxRecords {
		return nil, ErrQueueFull
	}
	return names, nil
}

// Due returns bounded lightweight entries, never materializes a batch of file
// payloads. A caller resolves clients only from its explicit peer configuration.
func (s *FileOutbox) Due(now time.Time, limit int) ([]DueEntry, error) {
	if limit < 1 || limit > 100 || !validClock(now.Unix()) {
		return nil, errors.New("invalid due page")
	}
	s.journal.mu.Lock()
	defer s.journal.mu.Unlock()
	names, err := s.entries()
	if err != nil {
		return nil, err
	}
	result := make([]DueEntry, 0)
	for _, name := range names {
		b, found, err := s.journal.read(name, MaxWire+4096)
		if err != nil || !found {
			return nil, errors.New("unreadable outbox entry")
		}
		var r Outbound
		if err = strictJSON(b, &r); err != nil {
			return nil, err
		}
		if name != recordName(s.domain, r.Packet.Envelope.ID) {
			return nil, errors.New("outbox filename mismatch")
		}
		r, found, err = s.get(r.Packet.Envelope.ID)
		if err != nil || !found {
			return nil, errors.New("invalid outbox entry")
		}
		if (r.State == Pending || r.State == Uncertain) && r.NextAttempt <= now.Unix() {
			result = append(result, DueEntry{ID: r.Packet.Envelope.ID, State: r.State, Destination: r.Packet.Envelope.To, NextAttempt: r.NextAttempt})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].NextAttempt == result[j].NextAttempt {
			return result[i].ID < result[j].ID
		}
		return result[i].NextAttempt < result[j].NextAttempt
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
func retryDelay(attempt int) int64 {
	cap := int64(300)
	if attempt < 9 {
		cap = 1 << attempt
	}
	return cap/2 + rand.Int64N(cap-cap/2+1)
}

// Dispatch first commits an uncertain attempt marker, then uses the original
// signed packet, and finally persists a verified receipt. No new IDs or fallback.
func (s *FileOutbox) Dispatch(ctx context.Context, id string, c *Client, now time.Time) (Outbound, error) {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	s.journal.mu.Lock()
	r, found, err := s.get(id)
	if err != nil || !found {
		s.journal.mu.Unlock()
		if err != nil {
			return Outbound{}, err
		}
		return Outbound{}, errors.New("outbox entry not found")
	}
	if r.State == Accepted || r.State == Expired {
		s.journal.mu.Unlock()
		return r, nil
	}
	clock := now.Unix()
	if !validClock(clock) || clock < r.Updated {
		s.journal.mu.Unlock()
		return Outbound{}, errors.New("outbox clock moved backwards")
	}
	if r.State == Blocked || clock < r.NextAttempt {
		s.journal.mu.Unlock()
		return r, ErrNotDue
	}
	if err = ctx.Err(); err != nil {
		s.journal.mu.Unlock()
		return r, err
	}
	if c == nil || c.destination != r.Packet.Envelope.To {
		s.journal.mu.Unlock()
		return r, errors.New("configured peer does not match queued packet")
	}
	if r.Attempts == 0 && r.Packet.Envelope.Expires <= clock {
		r.State = Expired
		r.NextAttempt = 0
		r.Updated = clock
		err = s.save(r)
		s.journal.mu.Unlock()
		if err != nil {
			return Outbound{}, err
		}
		return r, nil
	}
	key, known := s.keys[r.Packet.Envelope.KeyID]
	if !known || r.Packet.Verify(key) != nil || r.Attempts >= 1000000 {
		r.State = Blocked
		r.NextAttempt = 0
		r.Updated = clock
		err = s.save(r)
		s.journal.mu.Unlock()
		if err != nil {
			return Outbound{}, err
		}
		return r, errors.New("origin key unavailable or retry budget exceeded")
	}
	r.State = Uncertain
	r.Attempts++
	r.NextAttempt = clock + retryDelay(r.Attempts)
	r.Updated = clock
	r.LastStatus = 0
	err = s.save(r)
	s.journal.mu.Unlock()
	if err != nil {
		return Outbound{}, err
	}
	receipt, sendErr := c.Send(ctx, r.Packet)
	s.journal.mu.Lock()
	defer s.journal.mu.Unlock()
	if sendErr == nil {
		peer, known := c.keys[receipt.KeyID]
		if !known || receipt.Verify(r.Packet, peer) != nil {
			return r, errors.New("unverified peer acknowledgement")
		}
		peer.Public = append(ed25519.PublicKey(nil), peer.Public...)
		r.State = Accepted
		r.Receipt = &receipt
		r.ReceiptKey = &peer
		r.NextAttempt = 0
	} else {
		var status *HTTPError
		if errors.As(sendErr, &status) {
			r.LastStatus = status.Status
			if !status.Retryable {
				r.State = Blocked
				r.NextAttempt = 0
			}
		}
	}
	if err = s.save(r); err != nil {
		return Outbound{}, err
	}
	return r, sendErr
}

// Resume is explicit operator action, preserving bytes/ID and uncertain history.
func (s *FileOutbox) Resume(id string, now time.Time) (Outbound, error) {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	s.journal.mu.Lock()
	defer s.journal.mu.Unlock()
	r, found, err := s.get(id)
	if err != nil {
		return Outbound{}, err
	}
	if !found || r.State != Blocked {
		return r, ErrTerminal
	}
	clock := now.Unix()
	if !validClock(clock) || clock < r.Updated {
		return r, errors.New("invalid resume clock")
	}
	r.State = Pending
	if r.Attempts > 0 {
		r.State = Uncertain
	}
	r.NextAttempt = clock
	r.Updated = clock
	if err = s.save(r); err != nil {
		return Outbound{}, err
	}
	return r, nil
}
