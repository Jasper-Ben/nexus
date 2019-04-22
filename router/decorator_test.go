package router

import (
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/gammazero/nexus/wamp"
)

func TestDecorator(t *testing.T) {
	defer leaktest.Check(t)()

	r, _ := newTestRouter()
	defer r.Close()

	decoratorCallee, _ := testClient(r)
	regID := wamp.GlobalID()
	_ = decoratorCallee.Send(&wamp.Register{
		Request:   regID,
		Procedure: "decoratortest.handlerURI",
	})
	registered, err := wamp.RecvTimeout(decoratorCallee, time.Second)
	if err != nil {
		t.Error("failed to register, ", err)
		return
	}
	registerMessage, ok := registered.(*wamp.Registered)
	if !ok {
		t.Error("Router responded with: ", registered)
	}
	regID = registerMessage.Registration
	t.Log("Registered decorator function with regID:", regID)

	caller, _ := testClient(r)
	callID := wamp.GlobalID()
	_ = caller.Send(&wamp.Call{
		Request:   callID,
		Procedure: wamp.MetaProcDecoratorAdd,
		Arguments: wamp.List{
			"preprocess",
			"foo.test.bar",
			"exact",
			"decoratortest.handlerURI",
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
