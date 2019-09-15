package tipc

import (
	"net"
	"testing"

	"golang.org/x/net/nettest"
	"golang.org/x/sys/unix"
)

func pipemaker() (c1, c2 net.Conn, stop func(), err error) {
	sr := &unix.TIPCServiceRange{
		Type:  999,
		Lower: 0,
		Upper: ^uint32(0),
	}

	l, err := Listen(unix.TIPC_CLUSTER_SCOPE, sr)
	if err != nil {
		return nil, nil, nil, err
	}

	ready := make(chan int)

	go func() {
		var aerr error
		c2, aerr = l.Accept()
		if aerr != nil {
			panic(aerr)
		}
		close(ready)
	}()

	sa := &unix.TIPCServiceName{
		Type:     999,
		Instance: 0,
		Domain:   0,
	}

	st := &unix.SockaddrTIPC{
		Scope: unix.TIPC_CLUSTER_SCOPE,
		Addr:  sa,
	}

	c1, err = DialStream(st)
	if err != nil {
		return nil, nil, nil, err
	}

	<-ready

	stop = func() {
		l.Close()
		c2.Close()
		c1.Close()
	}

	return
}

func TestNetConn(t *testing.T) {
	nettest.TestConn(t, pipemaker)
}

func TestSocketPair(t *testing.T) {
	socketpair := func() (net.Conn, net.Conn, func(), error) {
		c1, c2, err := SocketPair()
		if err != nil {
			t.Fatal(err)
		}

		stop := func() {
			c2.Close()
			c1.Close()
		}

		return c1, c2, stop, nil
	}

	nettest.TestConn(t, socketpair)
}
