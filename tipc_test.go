package tipc

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/net/nettest"
	"golang.org/x/sync/errgroup"
)

func TestListen(t *testing.T) {
	var g errgroup.Group

	serv, inst := uint32(100), uint32(10)

	die := make(chan int)

	g.Go(func() error {
		c, err := DialService(serv, inst)
		if err != nil {
			fmt.Printf("dial: %v\n", err)
			return err
		}

		defer func() {
			if cerr := c.Close(); err != nil {
				fmt.Printf("close: %v\n", err)
				err = cerr
			}
		}()

		fmt.Printf("conn: %s\n", c)

		_, err = io.WriteString(c, "hello!")
		fmt.Printf("write: %v\n", err)

		<-die

		return err
	})

	defer func() {
		if err := g.Wait(); err != nil {
			t.Error(err)
		}
	}()

	defer close(die)

	l, err := Listen(100, 10)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("listening on %s", l.Addr())

	defer func() {
		if err := l.Close(); err != nil {
			t.Error(err)
		}
	}()

	c, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	}()

	buf := make([]byte, 128)

	if err := c.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}

	fmt.Printf("gonna read %v\n", c)

	n, err := c.Recvmsg(buf)
	if err != nil {
		t.Fatal(err)
	}

	buf = buf[:n]

	t.Logf("read %d: len(buf)=%d %s", n, len(buf), buf)
}

func pipemaker() (c1, c2 net.Conn, stop func(), err error) {
	l, err := Listen(999, 999)
	if err != nil {
		return nil, nil, nil, err
	}

	ready := make(chan int)

	go func() {
		var aerr error
		c2, aerr = l.Accept()
		if aerr != nil {
			fmt.Println(err)
			panic(aerr)
		}
		close(ready)
	}()

	c1, err = DialService(999, 999)
	if err != nil {
		return nil, nil, nil, err
	}

	<-ready

	stop = func() {
		l.Close()
		c1.Close()
		c2.Close()
	}

	return
}

/*
func sockpairmaker() (c1, c2 net.Conn, stop func(), err error) {
	socketpair(syscall.SOCK_STREAM)
}
*/

func TestNetConn(t *testing.T) {
	nettest.TestConn(t, pipemaker)
}
