package smip

type Record struct {
	Packet  Packet  `json:"packet"`
	Receipt Receipt `json:"receipt"`
}
type Inbox interface {
	Get(origin, id string) (Record, bool, error)
	Put(Record) (Record, bool, error)
}
