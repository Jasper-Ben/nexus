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
// TODO: Test with actual modified messages.

func TestBasicPreProcessDecorator(t *testing.T) {

	// This has to be a router, since a dealer would
	// not know about the meta API
	router, _ := newTestRouter()
	// These have to be a full-fledged testClients,
	// as we need to modify return values for extended testing
	callee, _ := newTestClient(router)
	caller, _ := newTestClient(router)
	ctx := context.Background()

	handler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		return &client.InvokeResult{}
	}
	// Register target URI
	if err := callee.Register("foo.test.bar", handler, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Register handler URI
	if err := callee.Register("decoratortest.handlerURI", handler, nil); err != nil {
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
	// As the preprocess handler returns nothing,
	// the call will be processed as usual.
	if _, err := caller.Call(ctx, "decoratortest.handlerURI", nil, nil, nil, ""); err != nil {
		t.Fatalf("Error calling preprocess decorator: %v\n", err)
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
//
//	// Test caller cancelling call. mode=kill
//	opts := wamp.SetOption(nil, "mode", "skip")
//	dealer.Cancel(callerSession, &wamp.Cancel{Request: 125, Options: opts})
//
//	// callee should NOT receive an INTERRUPT request
//	select {
//	case <-time.After(200 * time.Millisecond):
//	case msg := <-callee.Recv():
//		t.Fatal("callee received unexpected message:", msg.MessageType())
//	}
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
//}
//
//func TestSharedRegistrationRoundRobin(t *testing.T) {
//	dealer, metaClient := newTestDealer()
//
//	calleeRoles := wamp.Dict{
//		"roles": wamp.Dict{
//			"callee": wamp.Dict{
//				"features": wamp.Dict{
//					"shared_registration": true,
//				},
//			},
//		},
//	}
//
//	// Register callee1 with roundrobin shared registration
//	callee1 := newTestPeer()
//	calleeSess1 := newSession(callee1, 0, calleeRoles)
//	dealer.Register(calleeSess1, &wamp.Register{
//		Request:   123,
//		Procedure: testProcedure,
//		Options:   wamp.SetOption(nil, "invoke", "roundrobin"),
//	})
//	rsp := <-callee1.Recv()
//	regMsg, ok := rsp.(*wamp.Registered)
//	if !ok {
//		t.Fatal("did not receive REGISTERED response")
//	}
//	regID1 := regMsg.Registration
//	err := checkMetaReg(metaClient, calleeSess1.ID)
//	if err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//	if err = checkMetaReg(metaClient, calleeSess1.ID); err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//
//	// Register callee2 with roundrobin shared registration
//	callee2 := newTestPeer()
//	calleeSess2 := newSession(callee2, 0, calleeRoles)
//	dealer.Register(calleeSess2, &wamp.Register{
//		Request:   124,
//		Procedure: testProcedure,
//		Options:   wamp.SetOption(nil, "invoke", "roundrobin"),
//	})
//	rsp = <-callee2.Recv()
//	regMsg, ok = rsp.(*wamp.Registered)
//	if !ok {
//		t.Fatal("did not receive REGISTERED response")
//	}
//	if err = checkMetaReg(metaClient, calleeSess2.ID); err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//	regID2 := regMsg.Registration
//
//	if regID1 != regID2 {
//		t.Fatal("procedures should have same registration")
//	}
//
//	// Test calling valid procedure
//	caller := newTestPeer()
//	callerSession := newSession(caller, 0, nil)
//	dealer.Call(callerSession,
//		&wamp.Call{Request: 125, Procedure: testProcedure})
//
//	// Test that callee1 received an INVOCATION message.
//	var inv *wamp.Invocation
//	select {
//	case rsp = <-callee1.Recv():
//		inv, ok = rsp.(*wamp.Invocation)
//		if !ok {
//			t.Fatal("expected INVOCATION, got:", rsp.MessageType())
//		}
//	case rsp = <-callee2.Recv():
//		t.Fatal("should not have received from callee2")
//	case <-time.After(time.Second):
//		t.Fatal("Timed out waiting for INVOCATION")
//	}
//
//	// Callee responds with a YIELD message
//	dealer.Yield(calleeSess1, &wamp.Yield{Request: inv.Request})
//	// Check that caller received a RESULT message.
//	rsp = <-caller.Recv()
//	rslt, ok := rsp.(*wamp.Result)
//	if !ok {
//		t.Fatal("expected RESULT, got:", rsp.MessageType())
//	}
//	if rslt.Request != 125 {
//		t.Fatal("wrong request ID in RESULT")
//	}
//
//	// Test calling valid procedure
//	dealer.Call(callerSession,
//		&wamp.Call{Request: 126, Procedure: testProcedure})
//
//	// Test that callee2 received an INVOCATION message.
//	select {
//	case rsp = <-callee2.Recv():
//		inv, ok = rsp.(*wamp.Invocation)
//		if !ok {
//			t.Fatal("expected INVOCATION, got:", rsp.MessageType())
//		}
//	case rsp = <-callee1.Recv():
//		t.Fatal("should not have received from callee1")
//	case <-time.After(time.Second):
//		t.Fatal("Timed out waiting for INVOCATION")
//	}
//
//	// Callee responds with a YIELD message
//	dealer.Yield(calleeSess2, &wamp.Yield{Request: inv.Request})
//	// Check that caller received a RESULT message.
//	rsp = <-caller.Recv()
//	rslt, ok = rsp.(*wamp.Result)
//	if !ok {
//		t.Fatal("expected RESULT, got:", rsp.MessageType())
//	}
//	if rslt.Request != 126 {
//		t.Fatal("wrong request ID in RESULT")
//	}
//}
//
//func TestSharedRegistrationFirst(t *testing.T) {
//	dealer, metaClient := newTestDealer()
//
//	calleeRoles := wamp.Dict{
//		"roles": wamp.Dict{
//			"callee": wamp.Dict{
//				"features": wamp.Dict{
//					"shared_registration": true,
//				},
//			},
//		},
//	}
//
//	// Register callee1 with first shared registration
//	callee1 := newTestPeer()
//	calleeSess1 := newSession(callee1, 0, calleeRoles)
//	dealer.Register(calleeSess1, &wamp.Register{
//		Request:   123,
//		Procedure: testProcedure,
//		Options:   wamp.SetOption(nil, "invoke", "first"),
//	})
//	rsp := <-callee1.Recv()
//	regMsg, ok := rsp.(*wamp.Registered)
//	if !ok {
//		t.Fatal("did not receive REGISTERED response")
//	}
//	regID1 := regMsg.Registration
//	err := checkMetaReg(metaClient, calleeSess1.ID)
//	if err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//	if err = checkMetaReg(metaClient, calleeSess1.ID); err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//
//	// Register callee2 with roundrobin shared registration
//	callee2 := newTestPeer()
//	calleeSess2 := newSession(callee2, 0, calleeRoles)
//	dealer.Register(calleeSess2, &wamp.Register{
//		Request:   1233,
//		Procedure: testProcedure,
//		Options:   wamp.SetOption(nil, "invoke", "roundrobin"),
//	})
//	rsp = <-callee2.Recv()
//	_, ok = rsp.(*wamp.Error)
//	if !ok {
//		t.Fatal("expected ERROR response")
//	}
//
//	// Register callee2 with "first" shared registration
//	dealer.Register(calleeSess2, &wamp.Register{
//		Request:   124,
//		Procedure: testProcedure,
//		Options:   wamp.SetOption(nil, "invoke", "first"),
//	})
//	rsp = <-callee2.Recv()
//	if regMsg, ok = rsp.(*wamp.Registered); !ok {
//		t.Fatal("did not receive REGISTERED response")
//	}
//	regID2 := regMsg.Registration
//	if err = checkMetaReg(metaClient, calleeSess1.ID); err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//
//	if regID1 != regID2 {
//		t.Fatal("procedures should have same registration")
//	}
//
//	// Test calling valid procedure
//	caller := newTestPeer()
//	callerSession := newSession(caller, 0, nil)
//	dealer.Call(callerSession,
//		&wamp.Call{Request: 125, Procedure: testProcedure})
//
//	// Test that callee1 received an INVOCATION message.
//	var inv *wamp.Invocation
//	select {
//	case rsp = <-callee1.Recv():
//		inv, ok = rsp.(*wamp.Invocation)
//		if !ok {
//			t.Fatal("expected INVOCATION, got:", rsp.MessageType())
//		}
//	case rsp = <-callee2.Recv():
//		t.Fatal("should not have received from callee2")
//	case <-time.After(time.Second):
//		t.Fatal("Timed out waiting for INVOCATION")
//	}
//
//	// Callee responds with a YIELD message
//	dealer.Yield(calleeSess1, &wamp.Yield{Request: inv.Request})
//	// Check that caller received a RESULT message.
//	rsp = <-caller.Recv()
//	rslt, ok := rsp.(*wamp.Result)
//	if !ok {
//		t.Fatal("expected RESULT, got:", rsp.MessageType())
//	}
//	if rslt.Request != 125 {
//		t.Fatal("wrong request ID in RESULT")
//	}
//
//	// Test calling valid procedure
//	dealer.Call(callerSession,
//		&wamp.Call{Request: 126, Procedure: testProcedure})
//
//	// Test that callee1 received an INVOCATION message.
//	select {
//	case rsp = <-callee1.Recv():
//		inv, ok = rsp.(*wamp.Invocation)
//		if !ok {
//			t.Fatal("expected INVOCATION, got:", rsp.MessageType())
//		}
//	case rsp = <-callee2.Recv():
//		t.Fatal("should not have received from callee2")
//	case <-time.After(time.Second):
//		t.Fatal("Timed out waiting for INVOCATION")
//	}
//
//	// Callee responds with a YIELD message
//	dealer.Yield(calleeSess2, &wamp.Yield{Request: inv.Request})
//	// Check that caller received a RESULT message.
//	rsp = <-caller.Recv()
//	rslt, ok = rsp.(*wamp.Result)
//	if !ok {
//		t.Fatal("expected RESULT, got:", rsp.MessageType())
//	}
//	if rslt.Request != 126 {
//		t.Fatal("wrong request ID in RESULT")
//	}
//
//	// Remove callee1
//	dealer.RemoveSession(calleeSess1)
//	if err = checkMetaReg(metaClient, calleeSess1.ID); err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//	if err = checkMetaReg(metaClient, calleeSess1.ID); err == nil {
//		t.Fatal("Expected error")
//	}
//
//	// Test calling valid procedure
//	dealer.Call(callerSession,
//		&wamp.Call{Request: 127, Procedure: testProcedure})
//
//	// Test that callee2 received an INVOCATION message.
//	select {
//	case rsp = <-callee2.Recv():
//		inv, ok = rsp.(*wamp.Invocation)
//		if !ok {
//			t.Fatal("expected INVOCATION, got:", rsp.MessageType())
//		}
//	case rsp = <-callee1.Recv():
//		t.Fatal("should not have received from callee1")
//	case <-time.After(time.Second):
//		t.Fatal("Timed out waiting for INVOCATION")
//	}
//
//	// Callee responds with a YIELD message
//	dealer.Yield(calleeSess2, &wamp.Yield{Request: inv.Request})
//	// Check that caller received a RESULT message.
//	rsp = <-caller.Recv()
//	rslt, ok = rsp.(*wamp.Result)
//	if !ok {
//		t.Fatal("expected RESULT, got:", rsp.MessageType())
//	}
//	if rslt.Request != 127 {
//		t.Fatal("wrong request ID in RESULT")
//	}
//}
//
//func TestSharedRegistrationLast(t *testing.T) {
//	dealer, metaClient := newTestDealer()
//
//	calleeRoles := wamp.Dict{
//		"roles": wamp.Dict{
//			"callee": wamp.Dict{
//				"features": wamp.Dict{
//					"shared_registration": true,
//				},
//			},
//		},
//	}
//
//	// Register callee1 with last shared registration
//	callee1 := newTestPeer()
//	calleeSess1 := newSession(callee1, 0, calleeRoles)
//	dealer.Register(calleeSess1, &wamp.Register{
//		Request:   123,
//		Procedure: testProcedure,
//		Options:   wamp.SetOption(nil, "invoke", "last"),
//	})
//	rsp := <-callee1.Recv()
//	_, ok := rsp.(*wamp.Registered)
//	if !ok {
//		t.Fatal("did not receive REGISTERED response")
//	}
//	err := checkMetaReg(metaClient, calleeSess1.ID)
//	if err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//	if err = checkMetaReg(metaClient, calleeSess1.ID); err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//
//	// Register callee2 with last shared registration
//	callee2 := newTestPeer()
//	calleeSess2 := newSession(callee2, 0, calleeRoles)
//	dealer.Register(calleeSess2, &wamp.Register{
//		Request:   124,
//		Procedure: testProcedure,
//		Options:   wamp.SetOption(nil, "invoke", "last"),
//	})
//	rsp = <-callee2.Recv()
//	if _, ok = rsp.(*wamp.Registered); !ok {
//		t.Fatal("did not receive REGISTERED response")
//	}
//	if err = checkMetaReg(metaClient, calleeSess1.ID); err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//
//	// Test calling valid procedure
//	caller := newTestPeer()
//	callerSession := newSession(caller, 0, nil)
//	dealer.Call(callerSession,
//		&wamp.Call{Request: 125, Procedure: testProcedure})
//
//	// Test that callee2 received an INVOCATION message.
//	var inv *wamp.Invocation
//	select {
//	case rsp = <-callee2.Recv():
//		inv, ok = rsp.(*wamp.Invocation)
//		if !ok {
//			t.Fatal("expected INVOCATION, got:", rsp.MessageType())
//		}
//	case rsp = <-callee1.Recv():
//		t.Fatal("should not have received from callee1")
//	case <-time.After(time.Second):
//		t.Fatal("Timed out waiting for INVOCATION")
//	}
//
//	// Callee responds with a YIELD message
//	dealer.Yield(calleeSess2, &wamp.Yield{Request: inv.Request})
//	// Check that caller received a RESULT message.
//	rsp = <-caller.Recv()
//	rslt, ok := rsp.(*wamp.Result)
//	if !ok {
//		t.Fatal("expected RESULT, got:", rsp.MessageType())
//	}
//	if rslt.Request != 125 {
//		t.Fatal("wrong request ID in RESULT")
//	}
//
//	// Test calling valid procedure
//	dealer.Call(callerSession,
//		&wamp.Call{Request: 126, Procedure: testProcedure})
//
//	// Test that callee2 received an INVOCATION message.
//	select {
//	case rsp = <-callee2.Recv():
//		inv, ok = rsp.(*wamp.Invocation)
//		if !ok {
//			t.Fatal("expected INVOCATION, got:", rsp.MessageType())
//		}
//	case rsp = <-callee1.Recv():
//		t.Fatal("should not have received from callee1")
//	case <-time.After(time.Second):
//		t.Fatal("Timed out waiting for INVOCATION")
//	}
//
//	// Callee responds with a YIELD message
//	dealer.Yield(calleeSess2, &wamp.Yield{Request: inv.Request})
//	// Check that caller received a RESULT message.
//	rsp = <-caller.Recv()
//	rslt, ok = rsp.(*wamp.Result)
//	if !ok {
//		t.Fatal("expected RESULT, got:", rsp.MessageType())
//	}
//	if rslt.Request != 126 {
//		t.Fatal("wrong request ID in RESULT")
//	}
//
//	// Remove callee2
//	dealer.RemoveSession(calleeSess2)
//	if err = checkMetaReg(metaClient, calleeSess2.ID); err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//	if err = checkMetaReg(metaClient, calleeSess1.ID); err == nil {
//		t.Fatal("Expected error")
//	}
//
//	// Test calling valid procedure
//	dealer.Call(callerSession,
//		&wamp.Call{Request: 127, Procedure: testProcedure})
//
//	// Test that callee1 received an INVOCATION message.
//	select {
//	case rsp = <-callee1.Recv():
//		inv, ok = rsp.(*wamp.Invocation)
//		if !ok {
//			t.Fatal("expected INVOCATION, got:", rsp.MessageType())
//		}
//	case rsp = <-callee2.Recv():
//		t.Fatal("should not have received from callee2")
//	case <-time.After(time.Second):
//		t.Fatal("Timed out waiting for INVOCATION")
//	}
//
//	// Callee responds with a YIELD message
//	dealer.Yield(calleeSess1, &wamp.Yield{Request: inv.Request})
//	// Check that caller received a RESULT message.
//	rsp = <-caller.Recv()
//	rslt, ok = rsp.(*wamp.Result)
//	if !ok {
//		t.Fatal("expected RESULT, got:", rsp.MessageType())
//	}
//	if rslt.Request != 127 {
//		t.Fatal("wrong request ID in RESULT")
//	}
//}
//
//func TestPatternBasedRegistration(t *testing.T) {
//	dealer, metaClient := newTestDealer()
//
//	calleeRoles := wamp.Dict{
//		"roles": wamp.Dict{
//			"callee": wamp.Dict{
//				"features": wamp.Dict{
//					"shared_registration": true,
//				},
//			},
//		},
//	}
//
//	// Register a procedure with wildcard match.
//	callee := newTestPeer()
//	calleeSess := newSession(callee, 0, calleeRoles)
//	dealer.Register(calleeSess,
//		&wamp.Register{
//			Request:   123,
//			Procedure: testProcedureWC,
//			Options: wamp.Dict{
//				wamp.OptMatch: wamp.MatchWildcard,
//			},
//		})
//	rsp := <-callee.Recv()
//	_, ok := rsp.(*wamp.Registered)
//	if !ok {
//		t.Fatal("did not receive REGISTERED response")
//	}
//	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//
//	caller := newTestPeer()
//	callerSession := newSession(caller, 0, nil)
//
//	// Test calling valid procedure with full name.  Wildcard should match.
//	dealer.Call(callerSession,
//		&wamp.Call{Request: 125, Procedure: testProcedure})
//
//	// Test that callee received an INVOCATION message.
//	rsp = <-callee.Recv()
//	inv, ok := rsp.(*wamp.Invocation)
//	if !ok {
//		t.Fatal("expected INVOCATION, got:", rsp.MessageType())
//	}
//	details, ok := wamp.AsDict(inv.Details)
//	if !ok {
//		t.Fatal("INVOCATION missing details")
//	}
//	proc, _ := wamp.AsURI(details[wamp.OptProcedure])
//	if proc != testProcedure {
//		t.Error("INVOCATION has missing or incorrect procedure detail")
//	}
//
//	// Callee responds with a YIELD message
//	dealer.Yield(calleeSess, &wamp.Yield{Request: inv.Request})
//	// Check that caller received a RESULT message.
//	rsp = <-caller.Recv()
//	rslt, ok := rsp.(*wamp.Result)
//	if !ok {
//		t.Fatal("expected RESULT, got:", rsp.MessageType())
//	}
//	if rslt.Request != 125 {
//		t.Fatal("wrong request ID in RESULT")
//	}
//}
//
//func TestRPCBlockedSlowClientCall(t *testing.T) {
//	dealer, metaClient := newTestDealer()
//
//	// Register a procedure.
//	callee, rtr := transport.LinkedPeers()
//	calleeSess := newSession(rtr, 0, nil)
//	dealer.Register(calleeSess,
//		&wamp.Register{Request: 223, Procedure: testProcedure})
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
//	caller, rtr := transport.LinkedPeers()
//	callerSession := newSession(rtr, 0, nil)
//
//	for i := 0; i < 20; i++ {
//		fmt.Println("Calling", i)
//		// Test calling valid procedure
//		dealer.Call(callerSession,
//			&wamp.Call{Request: wamp.ID(i + 225), Procedure: testProcedure})
//	}
//
//	fmt.Println("Waiting for error")
//	// Test that caller received an ERROR message.
//	rsp = <-caller.Recv()
//	rslt, ok := rsp.(*wamp.Error)
//	if !ok {
//		t.Fatal("expected ERROR, got:", rsp.MessageType())
//	}
//	if rslt.Error != wamp.ErrNetworkFailure {
//		t.Fatal("wrong error, want", wamp.ErrNetworkFailure, "got", rslt.Error)
//	}
//}
//
//func TestCallerIdentification(t *testing.T) {
//	// Test disclose_caller
//	// Test disclose_me
//	dealer, metaClient := newTestDealer()
//
//	calleeRoles := wamp.Dict{
//		"roles": wamp.Dict{
//			"callee": wamp.Dict{
//				"features": wamp.Dict{
//					"caller_identification": true,
//				},
//			},
//		},
//	}
//
//	// Register a procedure, set option to request disclosing caller.
//	callee := newTestPeer()
//	calleeSess := newSession(callee, 0, calleeRoles)
//	dealer.Register(calleeSess,
//		&wamp.Register{
//			Request:   123,
//			Procedure: testProcedure,
//			Options:   wamp.Dict{"disclose_caller": true},
//		})
//	rsp := <-callee.Recv()
//	_, ok := rsp.(*wamp.Registered)
//	if !ok {
//		t.Fatal("did not receive REGISTERED response")
//	}
//	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//	if err := checkMetaReg(metaClient, calleeSess.ID); err != nil {
//		t.Fatal("Registration meta event fail:", err)
//	}
//
//	caller := newTestPeer()
//	callerID := wamp.ID(11235813)
//	callerSession := newSession(caller, callerID, nil)
//
//	// Test calling valid procedure with full name.  Widlcard should match.
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
//	// Test that invocation contains caller ID.
//	if id, _ := wamp.AsID(inv.Details["caller"]); id != callerID {
//		fmt.Println("===> details:", inv.Details)
//		t.Fatal("Did not get expected caller ID")
//	}
//}
