package topology

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestTopologySubscribe(t *testing.T) {
	c, err := Topology(0)
	if err != nil {
		t.Error(err)
	}

	defer c.Close()

	sub := &unix.TIPCSubscr{
		Seq:     unix.TIPCServiceRange{Type: 1, Lower: 0, Upper: ^uint32(0)},
		Timeout: 1000,
		Filter:  unix.TIPC_SUB_SERVICE,
	}

	if err := c.Subscribe(sub); err != nil {
		t.Error(err)
	}

	evt, err := c.ReadEvent()
	if err != nil {
		t.Error(err)
	}

	t.Logf("event: %+v", evt)
}
