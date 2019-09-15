package topology

import (
	"encoding/binary"

	"github.com/mischief/tipc"
	"golang.org/x/sys/unix"
)

type TopologyConn struct {
	conn *tipc.Conn
}

func (tc *TopologyConn) Subscribe(sub *unix.TIPCSubscr) error {
	return binary.Write(tc.conn, binary.BigEndian, sub)
}

func (tc *TopologyConn) ReadEvent() (*unix.TIPCEvent, error) {
	var e unix.TIPCEvent

	if err := binary.Read(tc.conn, binary.BigEndian, &e); err != nil {
		return nil, err
	}

	return &e, nil
}

func (tc *TopologyConn) Close() error {
	return tc.conn.Close()
}

// Topology server connection. Pass 0 as node for self.
func Topology(node uint32) (*TopologyConn, error) {
	sa := &unix.TIPCServiceName{
		Type:     unix.TIPC_TOP_SRV,
		Instance: unix.TIPC_TOP_SRV,
		Domain:   node,
	}

	st := &unix.SockaddrTIPC{
		Scope: unix.TIPC_CLUSTER_SCOPE,
		Addr:  sa,
	}

	c, err := tipc.DialSequentialPacket(st)
	if err != nil {
		return nil, err
	}

	return &TopologyConn{conn: c}, nil
}
