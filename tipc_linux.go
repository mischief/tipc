package tipc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type Addr struct {
	unix.Sockaddr
}

func (a *Addr) Network() string {
	return "tipc"
}

func (a *Addr) String() string {
	ta := a.Sockaddr.(*unix.SockaddrTIPC)
	switch sa := ta.Addr.(type) {
	default:
		panic("unknown addr")
	case *unix.TIPCSocketAddr:
		return fmt.Sprintf("port=%d,node=%x", sa.Ref, sa.Node)
	}

	_ = ta
	return fmt.Sprintf("%T %+v", ta.Addr, ta.Addr)
}

func Listen(scope int, s *unix.TIPCServiceRange) (*Listener, error) {
	sock, err := unix.Socket(unix.AF_TIPC, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}

	sa := &unix.SockaddrTIPC{
		Scope: scope,
		Addr:  s,
	}

	if err := unix.Bind(sock, sa); err != nil {
		unix.Close(sock)
		return nil, err
	}

	if err := unix.Listen(sock, 32); err != nil {
		unix.Close(sock)
		return nil, err
	}

	if err := unix.SetNonblock(sock, true); err != nil {
		return nil, err
	}

	conn, err := newConn(sock)
	if err != nil {
		return nil, err
	}

	return &Listener{conn: conn}, nil
}

type Listener struct {
	conn *Conn
}

func (l *Listener) Accept() (net.Conn, error) {
	var (
		newfd int
		err   error
		sa    unix.Sockaddr
	)

	cerr := l.conn.sc.Read(func(fd uintptr) bool {
		newfd, sa, err = unix.Accept(int(fd))

		return !errors.Is(err, unix.EAGAIN)
	})

	if cerr != nil {
		return nil, cerr
	}

	if err != nil {
		return nil, err
	}

	if err := unix.SetNonblock(int(newfd), true); err != nil {
		return nil, err
	}

	c, err := newConn(int(newfd))
	if err != nil {
		return nil, err
	}

	c.remote = &Addr{sa}

	return c, nil
}

func (l *Listener) Close() error {
	return l.conn.Close()
}

func (l *Listener) Addr() net.Addr {
	return l.conn.LocalAddr()
}

type Conn struct {
	fd        int
	fil       *os.File
	sc        syscall.RawConn
	closeOnce sync.Once

	addrmu sync.Mutex
	local  *Addr
	remote *Addr
}

func newConn(fd int) (*Conn, error) {
	fil := os.NewFile(uintptr(fd), "tipc")
	sc, err := fil.SyscallConn()
	if err != nil {
		fil.Close()
		return nil, err
	}

	return &Conn{fd: fd, fil: fil, sc: sc}, nil
}

func (c *Conn) String() string {
	return fmt.Sprintf("%s -> %s", c.LocalAddr(), c.RemoteAddr())
}

func (tc *Conn) Read(b []byte) (n int, err error) {
	n, err = tc.fil.Read(b)

	if err != nil {
		var perr *os.PathError
		if errors.As(err, &perr) {
			// XXX: io.Copy and friends expect io.EOF to cleanly
			// terminate, and tipc seems to indicate that with
			// ECONNRESET...
			if perr.Err == syscall.ECONNRESET {
				return n, io.EOF
			}
		}

		operr := &net.OpError{
			Op:     "read",
			Net:    "tipc",
			Source: tc.LocalAddr(),
			Addr:   tc.RemoteAddr(),
			Err:    err,
		}

		return n, operr
	}

	return
}

func (tc *Conn) Write(b []byte) (n int, err error) {
	n, err = tc.fil.Write(b)

	if err != nil {
		operr := &net.OpError{
			Op:     "write",
			Net:    "tipc",
			Source: tc.LocalAddr(),
			Addr:   tc.RemoteAddr(),
			Err:    err,
		}

		return 0, operr
	}

	return
}

func (tc *Conn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	/*
		// TODO: finish.
		var (
			iov unix.Iovec
			msg unix.Msghdr
			//sa        unix.RawSockaddrTIPC
			ancillary []byte
		)

		ancillary = make([]byte, unix.CmsgSpace(8)+unix.CmsgSpace(1024)+unix.CmsgSpace(16))

		iov.Base = &p[0]
		iov.SetLen(len(p))

		msg.Iov = &iov
		msg.Iovlen = 1

		msg.Control = &ancillary[0]
		msg.SetControllen(len(ancillary))

		return 0, nil, nil
	*/
	var (
		nn   int
		sa   unix.Sockaddr
		rerr error
	)

	cerr := tc.sc.Read(func(fd uintptr) bool {
		nn, sa, rerr = unix.Recvfrom(int(fd), p, 0)
		return !errors.Is(rerr, syscall.EAGAIN)
	})

	if cerr != nil {
		return 0, nil, cerr
	}

	if rerr != nil {
		return 0, nil, rerr
	}

	return nn, &Addr{sa}, nil
}

func (tc *Conn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	ta, ok := addr.(*Addr)
	if !ok {
		return 0, &net.AddrError{Err: "expected tipc.Addr", Addr: addr.String()}
	}

	cerr := tc.sc.Write(func(fd uintptr) bool {
		err = unix.Sendto(int(fd), p, 0, ta.Sockaddr)
		return !errors.Is(err, syscall.EAGAIN)
	})

	if cerr != nil {
		return 0, cerr
	}

	if err != nil {
		return 0, err
	}

	// TODO: implement.
	return len(p), nil
}

func (tc *Conn) Close() (err error) {
	return tc.fil.Close()
}

func (tc *Conn) LocalAddr() net.Addr {
	tc.addrmu.Lock()
	defer tc.addrmu.Unlock()

	if tc.local != nil {
		return tc.local
	}

	sa, err := unix.Getsockname(tc.fd)
	if err != nil {
		return nil
	}
	tc.local = &Addr{sa}

	return tc.local

	return &Addr{sa}
}

func (tc *Conn) RemoteAddr() net.Addr {
	tc.addrmu.Lock()
	defer tc.addrmu.Unlock()

	if tc.remote != nil {
		return tc.remote
	}

	sa, err := unix.Getpeername(tc.fd)
	if err != nil {
		return nil
	}

	tc.remote = &Addr{sa}

	return tc.remote
}

func (tc *Conn) SetDeadline(t time.Time) error {
	return tc.fil.SetDeadline(t)
}

func (tc *Conn) SetReadDeadline(t time.Time) error {
	return tc.fil.SetReadDeadline(t)
}

func (tc *Conn) SetWriteDeadline(t time.Time) error {
	return tc.fil.SetWriteDeadline(t)
}

func newConnectConn(typ int, s *unix.SockaddrTIPC) (*Conn, error) {
	fd, err := unix.Socket(unix.AF_TIPC, typ|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}

	if err := unix.Connect(fd, s); err != nil {
		unix.Close(fd)
		return nil, err
	}

	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return nil, err
	}

	return newConn(fd)
}

func DialSequentialPacket(s *unix.SockaddrTIPC) (*Conn, error) {
	return newConnectConn(unix.SOCK_SEQPACKET, s)
}

func DialStream(s *unix.SockaddrTIPC) (*Conn, error) {
	return newConnectConn(unix.SOCK_STREAM, s)
}

func newPacketConn(typ int, s *unix.SockaddrTIPC) (*Conn, error) {
	fd, err := unix.Socket(unix.AF_TIPC, typ|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}

	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return nil, err
	}

	if err := unix.Bind(fd, s); err != nil {
		unix.Close(fd)
		return nil, err
	}

	return newConn(fd)
}

func ListenReliableDatagram(s *unix.SockaddrTIPC) (*Conn, error) {
	return newPacketConn(unix.SOCK_RDM, s)
}
func ListenDatagram(s *unix.SockaddrTIPC) (*Conn, error) {
	return newPacketConn(unix.SOCK_DGRAM, s)
}

// SocketPair returns two AF_TIPC connections connected to each other through
// the local node. They are created as SOCK_SEQPACKET sockets.
func SocketPair() (c1, c2 *Conn, err error) {
	fds, err := unix.Socketpair(unix.AF_TIPC, unix.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}

	if err := unix.SetNonblock(fds[0], true); err != nil {
		unix.Close(fds[0])
		unix.Close(fds[1])
		return nil, nil, err
	}

	if err := unix.SetNonblock(fds[1], true); err != nil {
		unix.Close(fds[0])
		unix.Close(fds[1])
		return nil, nil, err
	}

	c1, err = newConn(fds[0])
	if err != nil {
		unix.Close(fds[0])
		unix.Close(fds[1])
		return nil, nil, err
	}

	c2, err = newConn(fds[1])
	if err != nil {
		unix.Close(fds[1])
		c1.Close()
		return nil, nil, err
	}

	return
}
