package smip

import "errors"

// ErrAdmissionDenied lets transactional stores recheck recipient policy at commit.
var ErrAdmissionDenied = errors.New("recipient or stream admission denied")

type Record struct {
	Packet  Packet  `json:"packet"`
	Receipt Receipt `json:"receipt"`
}
type Inbox interface {
	Get(origin, id string) (Record, bool, error)
	Put(Record) (Record, bool, error)
}
