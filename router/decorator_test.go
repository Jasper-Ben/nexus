package router

import (
	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/wamp"
	"testing"
	"time"
)

func TestDecorator(t *testing.T) {
	defer leaktest.Check(t)()

	r, _ := newTestRouter()
	defer r.Close()

	caller, _ := testClient(r)
	callID := wamp.GlobalID()
	_ = caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcDecoratorAdd,
		Arguments: wamp.List{
			"preprocess",
			"foo.test.bar",
			"exact",
			"decoratortest.handlerUri",
			0,
			"sync",
		},
	})

	replyMessage, _ := wamp.RecvTimeout(caller, time.Second)
	replyError, isOk := replyMessage.(*wamp.Error)

	if isOk {
		t.Error(replyError)
	}

	// TODO: Test a working example

}
