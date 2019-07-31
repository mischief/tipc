package topology

import (
	"encoding/binary"
	"errors"

	"github.com/mischief/tipc"
	"golang.org/x/sys/unix"
)

type Subscription struct {
	unix.TIPCServiceRange
	Timeout int32
	Filter  uint32

	// space for a user pointer - likely not needed in go.
	_usr [8]byte
}

func (s *Subscription) MarshalBinary() (b []byte, err error) {
	b = make([]byte, binary.Size(s))

	be := binary.BigEndian
	be.PutUint32(b[0:], s.Type)
	be.PutUint32(b[4:], s.Lower)
	be.PutUint32(b[8:], s.Upper)
	be.PutUint32(b[12:], uint32(s.Timeout))
	be.PutUint32(b[16:], s.Filter)

	return b, nil
}

func (s *Subscription) UnmarshalBinary(b []byte) error {
	if len(b) != binary.Size(s) {
		return errors.New("short event")
	}

	be := binary.BigEndian

	sr := &s.TIPCServiceRange

	sr.Type = be.Uint32(b[0:])
	sr.Lower = be.Uint32(b[4:])
	sr.Upper = be.Uint32(b[8:])

	s.Timeout = int32(be.Uint32(b[12:]))
	s.Filter = be.Uint32(b[16:])

	copy(s._usr[:], b[20:])

	return nil
}

//go:generate stringer -type=EventType

type EventType uint32

const (
	Published EventType = iota + 1
	Withdrawn
	Timeout
)

type Event struct {
	Event EventType
	Lower uint32
	Upper uint32
	unix.TIPCSocketAddr
	Subscription Subscription
}

func (e *Event) UnmarshalBinary(b []byte) error {
	if len(b) != binary.Size(e) {
		return errors.New("short event")
	}

	be := binary.BigEndian
	e.Event = EventType(be.Uint32(b[0:]))
	e.Lower = be.Uint32(b[4:])
	e.Upper = be.Uint32(b[8:])

	e.TIPCSocketAddr.Ref = be.Uint32(b[12:])
	e.TIPCSocketAddr.Node = be.Uint32(b[16:])

	return e.Subscription.UnmarshalBinary(b[20:])
}

type TopologyConn struct {
	conn *tipc.Conn
}

func (tc *TopologyConn) Subscribe(s *Subscription) error {
	buf, err := s.MarshalBinary()
	if err != nil {
		return err
	}

	if _, err := tc.conn.Write(buf); err != nil {
		return err
	}

	return nil
}

func (tc *TopologyConn) ReadEvent() (*Event, error) {
	var evt Event
	evtbuf := make([]byte, binary.Size(evt))
	if _, err := tc.conn.Read(evtbuf); err != nil {
		return nil, err
	}

	if err := evt.UnmarshalBinary(evtbuf); err != nil {
		return nil, err
	}

	return &evt, nil
}

func (tc *TopologyConn) Close() error {
	return tc.conn.Close()
}

// Topology server connection. Pass 0 as node for self.
func Topology(node uint32) (*TopologyConn, error) {
	sa := &unix.SockaddrTIPCAddrName{
		Name: unix.TIPCServiceAddr{
			Type:     1,
			Instance: 1,
		},
		Domain: node,
	}

	st := &unix.SockaddrTIPC{
		//Scope: unix.TIPC_CLUSTER_SCOPE,
		Addr: sa,
	}

	c, err := tipc.DialSequentialPacket(st)
	if err != nil {
		return nil, err
	}

	return &TopologyConn{conn: c}, nil
}
