package tipc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/xerrors"
)

const (
	sockaddrSize = 16

	// AddrTypes
	ServiceRange = 1
	ServiceAddr  = 2
	SocketAddr   = 3

	// Scopes
	ScopeZone    = 1
	ScopeCluster = 2
	ScopeNode    = 3

	TopSrv = 1
)

type Port struct {
	Ref  uint32
	Node uint32
}

type NameSeq struct {
	Type  uint32
	Lower uint32
	Upper uint32
}

const (
	SubPorts   = 1
	SubService = 2
	SubCancel  = 3

	WaitForever = ^uint32(0)
)

// shared sockaddr data
type Sockaddr struct {
	Family   uint16
	AddrType uint8
	Scope    int8
}

func (s *Sockaddr) Network() string {
	return "tipc"
}

func (s *Sockaddr) UnmarshalBinary(b []byte) error {
	if len(b) < binary.Size(s) {
		return errors.New("short sockaddr")
	}

	be := binary.BigEndian
	s.Family = be.Uint16(b[0:])
	s.AddrType = b[2]
	s.Scope = int8(b[3])

	return nil
}

type Service struct {
	Sockaddr
	Type     uint32
	Instance uint32
	Domain   uint32
}

/*
type ServiceRange struct {
	Sockaddr
	NameSeq
}
*/

type Peer struct {
	Sockaddr
	Instance uint32
	Node     uint32

	pad [4]byte
}

func (p *Peer) UnmarshalBinary(b []byte) error {
	if len(b) < binary.Size(p) {
		return errors.New("short peer")
	}

	if err := p.Sockaddr.UnmarshalBinary(b); err != nil {
		return err
	}

	b = b[4:]

	be := binary.BigEndian
	p.Instance = be.Uint32(b[0:])
	p.Node = be.Uint32(b[4:])

	return nil
}

func (p *Peer) String() string {
	return fmt.Sprintf("{%x,%x}", p.Instance, p.Node)
}

func Listen(stype, inst uint32) (*Listener, error) {
	serv := &Service{
		Sockaddr: Sockaddr{
			Family:   syscall.AF_TIPC,
			AddrType: ServiceAddr,
			Scope:    ScopeZone,
		},
		Type:     stype,
		Instance: inst,
	}

	sock, err := socket(syscall.SOCK_STREAM)
	if err != nil {
		return nil, xerrors.Errorf("socket: %w", err)
	}

	if _, _, errno := syscall.Syscall(syscall.SYS_BIND, uintptr(sock), uintptr(unsafe.Pointer(serv)), uintptr(sockaddrSize)); errno != 0 {
		return nil, xerrors.Errorf("bind: %w", errno)
	}

	if _, _, errno := syscall.Syscall(syscall.SYS_LISTEN, uintptr(sock), 0, 0); errno != 0 {
		return nil, xerrors.Errorf("listen: %w", errno)
	}

	if err := unblockfd(sock); err != nil {
		return nil, err
	}

	conn, err := newConn(sock)
	if err != nil {
		return nil, xerrors.Errorf("listen: %w", err)
	}

	return &Listener{conn: conn}, nil
}

type Listener struct {
	conn *Conn
}

func (l *Listener) Accept() (*Conn, error) {
	var (
		newfd uintptr
		errno syscall.Errno
	)

	cerr := l.conn.sc.Read(func(fd uintptr) bool {
		newfd, _, errno = syscall.Syscall(syscall.SYS_ACCEPT, fd, 0, 0)
		//fmt.Printf("accept %d = %d, %v\n", int(fd), int(newfd), errno)
		switch errno {
		case syscall.EAGAIN:
			return false
		}

		return true
	})

	if cerr != nil {
		return nil, xerrors.Errorf("accept: %w", cerr)
	}

	if errno != 0 {
		return nil, xerrors.Errorf("accept: %w", errno)
	}

	if err := unblockfd(int(newfd)); err != nil {
		return nil, err
	}

	c, err := newConn(int(newfd))
	if err != nil {
		return nil, err
	}

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
}

func newConn(fd int) (*Conn, error) {
	fil := os.NewFile(uintptr(fd), "tipc")
	sc, err := fil.SyscallConn()
	if err != nil {
		return nil, err
	}

	return &Conn{fd: fd, fil: fil, sc: sc}, nil
}

func (c *Conn) String() string {
	return fmt.Sprintf("%s -> %s", c.LocalAddr(), c.RemoteAddr())
}

func (tc *Conn) Recvmsg(b []byte) (int, error) {
	var (
		msg syscall.Msghdr
		iov syscall.Iovec
	)

	iov.Base = &b[0]
	iov.SetLen(len(b))

	buf := make([]byte, sockaddrSize*2)

	msg.Name = &buf[0]
	msg.Namelen = sockaddrSize * 2

	msg.Iov = &iov
	msg.Iovlen = 1

	rv, _, errno := syscall.Syscall(syscall.SYS_RECVMSG, uintptr(tc.fd), uintptr(unsafe.Pointer(&msg)), 0)
	if errno != 0 {
		return 0, errno
	}

	return int(rv), nil
}

func (tc *Conn) Read(b []byte) (n int, err error) {
	return tc.fil.Read(b)
}

func (tc *Conn) Write(b []byte) (n int, err error) {
	return tc.fil.Write(b)
}

func (tc *Conn) Close() (err error) {
	fmt.Printf("conn close %d\n", tc.fd)
	return tc.fil.Close()
}

func sockname(fd int, trap uintptr) (*Peer, error) {
	b := make([]byte, sockaddrSize)
	sh := (*reflect.SliceHeader)(unsafe.Pointer(&b))
	var sz int
	sz = sh.Len
	_, _, errno := syscall.Syscall(trap, uintptr(fd), sh.Data, uintptr(unsafe.Pointer(&sz)))
	if errno != 0 {
		return nil, errno
	}

	var p Peer

	if err := p.UnmarshalBinary(b); err != nil {
		return nil, err
	}

	return &p, nil
}

func (tc *Conn) LocalAddr() net.Addr {
	p, err := sockname(tc.fd, syscall.SYS_GETSOCKNAME)
	if err != nil {
		return nil
	}

	return p
}

func (tc *Conn) RemoteAddr() net.Addr {
	p, err := sockname(tc.fd, syscall.SYS_GETPEERNAME)
	if err != nil {
		return nil
	}

	return p
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

func DialService(stype, inst uint32) (*Conn, error) {
	fd, err := socket(syscall.SOCK_STREAM)
	if err != nil {
		return nil, err
	}

	sockaddr := Sockaddr{
		Family:   syscall.AF_TIPC,
		AddrType: ServiceAddr,
	}

	addr := &Service{
		Sockaddr: sockaddr,
		Type:     stype,
		Instance: inst,
	}

	if _, _, errno := syscall.Syscall(syscall.SYS_CONNECT, uintptr(fd), uintptr(unsafe.Pointer(addr)), uintptr(sockaddrSize)); errno != 0 {
		return nil, xerrors.Errorf("connect: %w", errno)
	}

	if err := unblockfd(fd); err != nil {
		return nil, err
	}

	c, err := newConn(fd)
	if err != nil {
		return nil, err
	}

	return c, nil
}

func unblockfd(fd int) error {
	return syscall.SetNonblock(fd, true)
}

func unblock(sc syscall.RawConn) (err error) {
	cerr := sc.Control(func(fd uintptr) {
		err = syscall.SetNonblock(int(fd), true)
	})

	if cerr != nil {
		err = cerr
	}

	return
}

func socket(typ int) (int, error) {
	fd, err := syscall.Socket(syscall.AF_TIPC, typ, 0)
	if err != nil {
		return -1, err
	}

	return fd, err
}

func socketpair(typ int) (fds [2]int, err error) {
	fds, err = syscall.Socketpair(syscall.AF_TIPC, typ, 0)
	if err != nil {
		return
	}

	if err = syscall.SetNonblock(fds[0], true); err != nil {
		syscall.Close(fds[0])
		syscall.Close(fds[1])
		return
	}

	if err = syscall.SetNonblock(fds[1], true); err != nil {
		syscall.Close(fds[0])
		syscall.Close(fds[1])
		return
	}

	return fds, nil
}
