package topology

import (
	"testing"

	"github.com/mischief/tipc"
)

func TestTopologySubscribe(t *testing.T) {
	c, err := Topology()
	if err != nil {
		t.Error(err)
	}

	defer c.Close()

	sub := &Subscription{
		NameSeq: tipc.NameSeq{
			Type:  18888,
			Lower: 17,
			Upper: 17,
		},
		Timeout: 1000,
		Filter:  tipc.SubService,
	}

	if err := c.Subscribe(sub); err != nil {
		t.Error(err)
	}

	evt, err := c.ReadEvent()
	if err != nil {
		t.Error(err)
	}

	t.Logf("event: %v", evt)
}
