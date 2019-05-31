// package router_test to avoid import cycle
// TODO: modify existing dealer_test.go to work from the router_test package
package router_test

import (
	"context"
	_ "errors"
	_ "fmt"
	"testing"
	"time"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/wamp"
)

var (
	debug  bool
	logger stdlog.StdLog
)

var clientRoles = wamp.Dict{
	"roles": wamp.Dict{
		"subscriber": wamp.Dict{
			"features": wamp.Dict{
				"publisher_identification": true,
			},
		},
		"publisher": wamp.Dict{
			"features": wamp.Dict{
				"subscriber_blackwhite_listing": true,
			},
		},
		"callee": wamp.Dict{},
		"caller": wamp.Dict{
			"features": wamp.Dict{
				"call_timeout": true,
			},
		},
	},
	"authmethods": wamp.List{"anonymous", "ticket"},
}

func newTestRouter() (router.Router, error) {
	config := &router.Config{
		RealmConfigs: []*router.RealmConfig{
			{
				URI:              wamp.URI("nexus.test.realm"),
				StrictURI:        false,
				AnonymousAuth:    true,
				AllowDisclose:    false,
				EnableMetaKill:   true,
				EnableMetaModify: true,
			},
		},
		Debug: debug,
	}
	return router.NewRouter(config, logger)
}

func newTestClient(r router.Router) (*client.Client, error) {
	cfg := client.Config{
		Realm:           "nexus.test.realm",
		ResponseTimeout: 500 * time.Millisecond,
		Logger:          logger,
		Debug:           false,
	}
	return client.ConnectLocal(r, cfg)
}

// --- Decorator Testing ---

func TestBasicPreProcessDecorator(t *testing.T) {

	// This has to be a router, since a dealer would
	// not know about the meta API
	router, _ := newTestRouter()
	// These have to be a full-fledged testClients,
	// as we need to modify return values for extended testing
	callee, _ := newTestClient(router)
	caller, _ := newTestClient(router)
	ctx := context.Background()
	done := make(chan bool, 1)

	handlerFail := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		t.Fatal("Error: unexpected RI! Call not modified.")
		return &client.InvokeResult{}
	}
	handlerSuccess := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		done <- true
		return &client.InvokeResult{}
	}

	decoratorHandler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		returnCall := &wamp.Call{
			Request:     args[0].(*wamp.Call).Request,
			Options:     wamp.Dict{},
			Procedure:   wamp.URI("test.rewrite.URI"),
			Arguments:   wamp.List{},
			ArgumentsKw: wamp.Dict{},
		}
		t.Log("running decoratorHandler")
		return &client.InvokeResult{
			Args: wamp.List{returnCall},
		}
	}
	// Register old target URI
	if err := callee.Register("foo.test.bar", handlerFail, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Register target URI
	if err := callee.Register("test.rewrite.URI", handlerSuccess, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Register handler URI
	if err := callee.Register("decoratortest.handlerURI", decoratorHandler, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Add preprocess decorator
	args := wamp.List{
		"preprocess",
		"foo.test.bar",
		"exact",
		"decoratortest.handlerURI",
		0,
		"sync"}

	if _, err := callee.Call(ctx, "wamp.decorator.add", nil, args, nil, ""); err != nil {
		t.Fatalf("Error calling wamp.decorator.add: %v\n", err)
	}

	// Trigger preprocess decorator
	_, err := caller.Call(ctx, "foo.test.bar", nil, nil, nil, "")
	if err != nil {
		t.Fatalf("Error calling preprocess decorator: %v\n", err)
	}

	go func() {
		time.Sleep(5 * time.Second)
		done <- false
	}()
	if !<-done {
		// TODO: fixme
		t.Fatalf("Call not redirected after 5 seconds!")
	}
}

//func TestBasicPrecallDecorator(t *testing.T) {
//
//	// This has to be a router, since a dealer would
//	// not know about the meta API
//	router, _ := newTestRouter()
//	// These have to be a full-fledged testClient
//	// as we need to modify return values for extended testing
//	callee, _ := newTestClient(router)
//	caller, _ := newTestClient(router)
//
//	// Register target URI
//	_ = callee.Send(&wamp.Register{
//		Request:   123,
//		Procedure: wamp.URI("decoratortest.handlerURI"),
//	})
//	rsp := <-callee.Recv()
//	_, ok := rsp.(*wamp.Registered)
//	if !ok {
//		t.Fatal("did not receive REGISTERED response")
//	}
//
//	// Register precall matchURI
//	_ = callee.Send(&wamp.Register{
//		Request:   127,
//		Procedure: wamp.URI("foo.test.bar.precall"),
//	})
//	rsp = <-callee.Recv()
//	_, ok = rsp.(*wamp.Registered)
//	if !ok {
//		t.Fatal("did not receive REGISTERED response")
//	}
//
//	// Add precall decorator
//	_ = callee.Send(&wamp.Call{
//		Request:   128,
//		Procedure: wamp.MetaProcDecoratorAdd,
//		Arguments: wamp.List{
//			"precall",
//			"foo.test.bar.precall",
//			"exact",
//			"decoratortest.handlerURI",
//			0,
//			"sync",
//		},
//	})
//	rsp = <-callee.Recv()
//	_, ok = rsp.(*wamp.Result)
//	if !ok {
//		t.Fatal("expected RESULT response, got:", rsp.MessageType())
//	}
//
//	// Trigger precall decorator
//	// as the precall handler returns nothing,
//	// the call will be processed as usual.
//	_ = caller.Send(&wamp.Call{Request: 129, Procedure: wamp.URI("foo.test.bar.precall")})
//	rsp = <-callee.Recv()
//	_, ok = rsp.(*wamp.Invocation)
//	if !ok {
//		t.Fatal("expected INVOCATION, got:", rsp.MessageType())
//	}
//}

//func TestBasicPublishDecorator(t *testing.T) {
//	router, _ := newTestRouter()
//	subscriber, _ := testClient(router)
//	testTopic := wamp.URI("nexus.test.topic")
//
//	_ = subscriber.Send(&wamp.Register{
//		Request:   123,
//		Procedure: wamp.URI("decoratortest.handlerURI"),
//	})
//	rsp := <-subscriber.Recv()
//	_, ok := rsp.(*wamp.Registered)
//	if !ok {
//		t.Fatal("did not receive REGISTERED response")
//	}
//
//	subscriber.Send(&wamp.Subscribe{Request: 123, Topic: testTopic})
//	rsp = <-subscriber.Recv()
//	_, ok = rsp.(*wamp.Subscribed)
//	if !ok {
//		t.Fatal("expected SUBSCRIBED response, got:", rsp.MessageType())
//	}
//
//	_ = subscriber.Send(&wamp.Call{
//		Request:   124,
//		Procedure: wamp.MetaProcDecoratorAdd,
//		Arguments: wamp.List{
//			"publish",
//			"nexus.test.topic",
//			"exact",
//			"decoratortest.handlerURI",
//			0,
//			"sync",
//		},
//	})
//	rsp = <-subscriber.Recv()
//	_, ok = rsp.(*wamp.Result)
//	if !ok {
//		t.Fatal("expected RESULT response, got:", rsp.MessageType())
//	}
//
//}
//
//// ---
//
//func TestCancelCallModeKill(t *testing.T) {
//	dealer, metaClient := newTestDealer()
//
//	calleeRoles := wamp.Dict{
//		"roles": wamp.Dict{
//			"callee": wamp.Dict{
//				"features": wamp.Dict{
//					"call_canceling": true,
//				},
//			},
//		},
//	}
//
//	calleeRolesNoCancel := wamp.Dict{
//		"roles": wamp.Dict{
//			"callee": wamp.Dict{
//				"features": wamp.Dict{
//					"progressive_call_results": true,
//					"call_canceling":           false,
//				},
//			},
//		},
//	}
//
//	// Register a procedure.
//	callee := newTestPeer()
//	calleeSess := newSession(callee, 0, calleeRoles)
//	dealer.Register(calleeSess,
//		&wamp.Register{Request: 123, Procedure: testProcedure})
//	rsp := <-callee.Recv()
//	_, ok := rsp.(*wamp.Registered)
//	if !ok {
//		t.Fatal("did not receive REGISTERED response")
//	}
//
//	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//
//	caller := newTestPeer()
//	callerSession := newSession(caller, 0, nil)
//
//	// Test calling valid procedure
//	dealer.Call(callerSession,
//		&wamp.Call{Request: 125, Procedure: testProcedure})
//
//	// Test that callee received an INVOCATION message.
//	rsp = <-callee.Recv()
//	inv, ok := rsp.(*wamp.Invocation)
//	if !ok {
//		t.Fatal("expected INVOCATION, got:", rsp.MessageType())
//	}
//
//	// Test caller cancelling call. This mode does not exist and the call is invalid
//	opts := wamp.SetOption(nil, "mode", "killnow")
//	dealer.Cancel(callerSession, &wamp.Cancel{Request: 125, Options: opts})
//	rsp = <-caller.Recv()
//	errMsg := rsp.(*wamp.Error)
//	if errMsg.Error != wamp.ErrInvalidArgument {
//		t.Error("expected error:", wamp.ErrInvalidArgument)
//	}
//
//	// Test caller cancelling call. mode=kill
//	opts = wamp.SetOption(nil, "mode", "kill")
//	dealer.Cancel(callerSession, &wamp.Cancel{Request: 125, Options: opts})
//
//	// callee should receive an INTERRUPT request
//	rsp = <-callee.Recv()
//	interrupt, ok := rsp.(*wamp.Interrupt)
//	if !ok {
//		t.Fatal("callee expected INTERRUPT, got:", rsp.MessageType())
//	}
//	if interrupt.Request != inv.Request {
//		t.Fatal("INTERRUPT request ID does not match INVOCATION request ID")
//	}
//
//	// callee responds with ERROR message
//	dealer.Error(calleeSess, &wamp.Error{
//		Type:    wamp.INVOCATION,
//		Request: inv.Request,
//		Error:   wamp.ErrCanceled,
//		Details: wamp.Dict{"reason": "callee canceled"},
//	})
//
//	// Check that caller receives the ERROR message.
//	rsp = <-caller.Recv()
//	rslt, ok := rsp.(*wamp.Error)
//	if !ok {
//		t.Fatal("expected ERROR, got:", rsp.MessageType())
//	}
//	if rslt.Error != wamp.ErrCanceled {
//		t.Fatal("wrong error, want", wamp.ErrCanceled, "got", rslt.Error)
//	}
//	if len(rslt.Details) == 0 {
//		t.Fatal("expected details in message")
//	}
//	if s, _ := wamp.AsString(rslt.Details["reason"]); s != "callee canceled" {
//		t.Fatal("Did not get error message from caller")
//	}
//
//	// Register a procedure with progressive calls but no call canceling support
//	calleeSessNoCancel := newSession(callee, 0, calleeRolesNoCancel)
//	dealer.Register(calleeSessNoCancel, &wamp.Register{Request: 124, Procedure: testProcedure})
//}
//
//func TestCancelCallModeKillNoWait(t *testing.T) {
//	dealer, metaClient := newTestDealer()
//
//	calleeRoles := wamp.Dict{
//		"roles": wamp.Dict{
//			"callee": wamp.Dict{
//				"features": wamp.Dict{
//					"call_canceling": true,
//				},
//			},
//		},
//	}
//
//	// Register a procedure.
//	callee := newTestPeer()
//	calleeSess := newSession(callee, 0, calleeRoles)
//	dealer.Register(calleeSess,
//		&wamp.Register{Request: 123, Procedure: testProcedure})
//	rsp := <-callee.Recv()
//	_, ok := rsp.(*wamp.Registered)
//	if !ok {
//		t.Fatal("did not receive REGISTERED response")
//	}
//
//	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//
//	caller := newTestPeer()
//	callerSession := newSession(caller, 0, nil)
//
//	// Test calling valid procedure
//	dealer.Call(callerSession,
//		&wamp.Call{Request: 125, Procedure: testProcedure})
//
//	// Test that callee received an INVOCATION message.
//	rsp = <-callee.Recv()
//	inv, ok := rsp.(*wamp.Invocation)
//	if !ok {
//		t.Fatal("expected INVOCATION, got:", rsp.MessageType())
//	}
//
//	// Test caller cancelling call. mode=kill
//	opts := wamp.SetOption(nil, "mode", "killnowait")
//	dealer.Cancel(callerSession, &wamp.Cancel{Request: 125, Options: opts})
//
//	// callee should receive an INTERRUPT request
//	rsp = <-callee.Recv()
//	interrupt, ok := rsp.(*wamp.Interrupt)
//	if !ok {
//		t.Fatal("callee expected INTERRUPT, got:", rsp.MessageType())
//	}
//	if interrupt.Request != inv.Request {
//		t.Fatal("INTERRUPT request ID does not match INVOCATION request ID")
//	}
//
//	// callee responds with ERROR message
//	dealer.Error(calleeSess, &wamp.Error{
//		Type:    wamp.INVOCATION,
//		Request: inv.Request,
//		Error:   wamp.ErrCanceled,
//		Details: wamp.Dict{"reason": "callee canceled"},
//	})
//
//	// Check that caller receives the ERROR message.
//	rsp = <-caller.Recv()
//	rslt, ok := rsp.(*wamp.Error)
//	if !ok {
//		t.Fatal("expected ERROR, got:", rsp.MessageType())
//	}
//	if rslt.Error != wamp.ErrCanceled {
//		t.Fatal("wrong error, want", wamp.ErrCanceled, "got", rslt.Error)
//	}
//	if len(rslt.Details) != 0 {
//		t.Fatal("should not have details; result should not be from callee")
//	}
//}
//
//func TestCancelCallModeSkip(t *testing.T) {
//	dealer, metaClient := newTestDealer()
//
//	// Register a procedure.
//	callee := newTestPeer()
//	calleeRoles := wamp.Dict{
//		"roles": wamp.Dict{
//			"callee": wamp.Dict{
//				"features": wamp.Dict{
//					"call_canceling": true,
//				},
//			},
//		},
//	}
//
//	calleeSess := newSession(callee, 0, calleeRoles)
//	dealer.Register(calleeSess,
//		&wamp.Register{Request: 123, Procedure: testProcedure})
//	rsp := <-callee.Recv()
//	_, ok := rsp.(*wamp.Registered)
//	if !ok {
//		t.Fatal("did not receive REGISTERED response")
//	}
//
//	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//
//	caller := newTestPeer()
//	callerSession := newSession(caller, 0, nil)
//
//	// Test calling valid procedure
//	dealer.Call(callerSession,
//		&wamp.Call{Request: 125, Procedure: testProcedure})
//
//	// Test that callee received an INVOCATION message.
//	rsp = <-callee.Recv()
//	_, ok = rsp.(*wamp.Invocation)
//	if !ok {
//		t.Fatal("expected INVOCATION, got:", rsp.MessageType())
//	}
