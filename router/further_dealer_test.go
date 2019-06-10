// package router_test to avoid import cycle
// TODO: modify existing dealer_test.go to work from the router_test package
package router_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gammazero/nexus/client"
	"github.com/gammazero/nexus/router"
	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport/serialize"
	"github.com/gammazero/nexus/wamp"
)

// unfortunately we have to redo code from dealer_test...

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

func TestPreProcessDecoratorErrorResult(t *testing.T) {
	router, _ := newTestRouter()
	callee, _ := newTestClient(router)
	decoratee, _ := newTestClient(router)
	caller, _ := newTestClient(router)
	ctx := context.Background()

	// test pre-process decorator with error return.
	handlerFooBar := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		t.Fatal("Error: unexpected URI, should return Error!")
		return &client.InvokeResult{}
	}

	decoratorHandler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		// handler returns an ERROR message: it should be sent to the caller.
		t.Logf("Returning error\n")
		return &client.InvokeResult{
			Err: wamp.URI("wamp.ErrCanceled"),
		}
	}
	// Register target URI (never used, since ERROR)
	if err := callee.Register("foo.test.bar.error", handlerFooBar, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Register handler URI
	if err := decoratee.Register("decoratortest.handlerURI.error", decoratorHandler, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Add preprocess decorator
	args := wamp.List{
		"preprocess",
		"foo.test.bar.error",
		"exact",
		"decoratortest.handlerURI.error",
		0,
		"sync"}
	if _, err := decoratee.Call(ctx, "wamp.decorator.add", nil, args, nil, ""); err != nil {
		t.Fatalf("Error calling wamp.decorator.add: %v\n", err)
	}

	// Trigger preprocess decorator
	rsp, err := caller.Call(ctx, "foo.test.bar.error", nil, nil, nil, "")
	rspp, _ := json.MarshalIndent(rsp, "", "  ")
	t.Logf("rsp is: %s\n", rspp)
	if err == nil {
		t.Fatalf("Expected an ERROR, got %v\n", rsp)
	}
	t.Logf("Got error: %v\n", err)
}

func TestPreProcessDecoratorOrder(t *testing.T) {
	router, _ := newTestRouter()
	callee, _ := newTestClient(router)
	decoratee, _ := newTestClient(router)
	caller, _ := newTestClient(router)
	ctx := context.Background()
	done := make(chan string, 3)

	// test pre-process decorators with order.
	handlerFooBar := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		return &client.InvokeResult{}
	}

	decoratorHandler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		// handler should be called first, as associated decorator has the smaller order number.
		t.Log("Called decorator handler, order: 0.")
		done <- "one"
		return &client.InvokeResult{}
	}
	decoratorHandler2 := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		// handler should be called second, as associated decorator has the larger order number.
		t.Log("Called decorator handler, order: 1.")
		done <- "two"
		return &client.InvokeResult{}
	}
	decoratorHandler3 := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		// handler should be called third, as associated decorator has the larger order number.
		t.Log("Called decorator handler, order: 2.")
		done <- "three"
		return &client.InvokeResult{}
	}

	// Register target URI
	if err := callee.Register("foo.test.bar.order", handlerFooBar, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Register handler URI3
	if err := decoratee.Register("decoratortest.handlerURI.three", decoratorHandler3, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}
	// Register handler URI1
	if err := decoratee.Register("decoratortest.handlerURI.one", decoratorHandler, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}
	// Register handler URI2
	if err := decoratee.Register("decoratortest.handlerURI.two", decoratorHandler2, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Add preprocess decorator2
	args := wamp.List{
		"preprocess",
		"foo.test.bar.order",
		"exact",
		"decoratortest.handlerURI.two",
		1,
		"sync"}
	if _, err := decoratee.Call(ctx, "wamp.decorator.add", nil, args, nil, ""); err != nil {
		t.Fatalf("Error calling wamp.decorator.add: %v\n", err)
	}
	// Add preprocess decorator1
	args = wamp.List{
		"preprocess",
		"foo.test.bar.order",
		"exact",
		"decoratortest.handlerURI.one",
		0,
		"sync"}
	if _, err := decoratee.Call(ctx, "wamp.decorator.add", nil, args, nil, ""); err != nil {
		t.Fatalf("Error calling wamp.decorator.add: %v\n", err)
	}
	// Add preprocess decorator3
	args = wamp.List{
		"preprocess",
		"foo.test.bar.order",
		"exact",
		"decoratortest.handlerURI.three",
		2,
		"sync"}
	if _, err := decoratee.Call(ctx, "wamp.decorator.add", nil, args, nil, ""); err != nil {
		t.Fatalf("Error calling wamp.decorator.add: %v\n", err)
	}

	// Trigger preprocess decorators
	rsp, err := caller.Call(ctx, "foo.test.bar.order", nil, nil, nil, "")
	rspp, _ := json.MarshalIndent(rsp, "", "  ")
	t.Logf("rsp is: %s\n", rspp)
	if err != nil {
		t.Fatalf("Error: %v\n", err)
	}
	if <-done != "one" {
		t.Fatal("Error: wrong order")
	}
	if <-done != "two" {
		t.Fatal("Error: wrong order")
	}
	if <-done != "three" {
		t.Fatal("Error: wrong order")
	}
}

func TestPreProcessDecoratorEmptyResult(t *testing.T) {
	router, _ := newTestRouter()
	callee, _ := newTestClient(router)
	decoratee, _ := newTestClient(router)
	caller, _ := newTestClient(router)
	ctx := context.Background()
	done := make(chan bool, 1)

	// test proprocess decorator: return nothing
	handlerFooBar := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		done <- true
		return &client.InvokeResult{}
	}

	decoratorHandler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		t.Log("Running decoratorHandler")
		// returning nothing: the call should be processed as usual
		return &client.InvokeResult{}
	}

	// Register old target URI
	if err := callee.Register("foo.test.bar.nothing", handlerFooBar, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Register handler URI
	if err := decoratee.Register("decoratortest.handlerURI.nothing", decoratorHandler, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Add preprocess decorator
	args := wamp.List{
		"preprocess",
		"foo.test.bar.nothing",
		"exact",
		"decoratortest.handlerURI.nothing",
		0,
		"sync"}
	if _, err := decoratee.Call(ctx, "wamp.decorator.add", nil, args, nil, ""); err != nil {
		t.Fatalf("Error calling wamp.decorator.add: %v\n", err)
	}

	// Trigger preprocess decorator
	rsp, err := caller.Call(ctx, "foo.test.bar.nothing", nil, nil, nil, "")
	if err != nil {
		t.Fatalf("Error calling preprocess decorator: %v\n", err)
	}
	rspp, _ := json.MarshalIndent(rsp, "", "  ")
	t.Logf("rsp is: %s\n", rspp)
	go func() {
		time.Sleep(5 * time.Second)
		done <- false
	}()
	if !<-done {
		t.Fatal("Preprocess Decorator: HandlerFooBar not called after 5 seconds!")
	}
}

func TestPreProcessDecoratorRedirectResult(t *testing.T) {
	router, _ := newTestRouter()
	callee, _ := newTestClient(router)
	decoratee, _ := newTestClient(router)
	caller, _ := newTestClient(router)
	ctx := context.Background()
	done := make(chan bool, 1)

	handlerFooBar := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		t.Fatal("Error: unexpected URI, should return modified URI!")
		return &client.InvokeResult{}
	}

	handlerSuccess := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		t.Logf("Got to the rewritten handler, %v, %v, %v\n", args, kwargs, details)
		done <- true
		return &client.InvokeResult{}
	}

	decoratorHandler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		returnCall := serialize.WampMessageToList(&wamp.Call{
			Request:     args[0].(*wamp.Call).Request,
			Options:     wamp.Dict{},
			Procedure:   wamp.URI("foo.rewrite.bar"),
			Arguments:   wamp.List{},
			ArgumentsKw: wamp.Dict{},
		})
		t.Log("running decoratorHandler")
		return &client.InvokeResult{
			// pre-process handler returns a new CALL message: it should be used instead of the original CALL message
			Args: wamp.List{returnCall},
		}
	}
	// Register old target URI
	if err := callee.Register("foo.test.bar.rewrite", handlerFooBar, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Register target URI
	if err := callee.Register("foo.rewrite.bar", handlerSuccess, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Register handler URI
	if err := decoratee.Register("decoratortest.handlerURI", decoratorHandler, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Add preprocess decorator
	args := wamp.List{
		"preprocess",
		"foo.test.bar.rewrite",
		"exact",
		"decoratortest.handlerURI",
		0,
		"sync"}

	if _, err := decoratee.Call(ctx, "wamp.decorator.add", nil, args, nil, ""); err != nil {
		t.Fatalf("Error calling wamp.decorator.add: %v\n", err)
	}

	// Trigger preprocess decorator
	rsp, err := caller.Call(ctx, "foo.test.bar.rewrite", nil, nil, nil, "")
	if err != nil {
		t.Fatalf("Error calling preprocess decorator: %v\n", err)
	}
	rspp, _ := json.MarshalIndent(rsp, "", "  ")
	t.Logf("rsp is: %s\n", rspp)
	go func() {
		time.Sleep(5 * time.Second)
		done <- false
	}()
	if !<-done {
		t.Fatal("Preprocess Decorator: Call not redirected after 5 seconds!")
	}
}

func TestPreProcessDecoratorAsync(t *testing.T) {
	router, _ := newTestRouter()
	callee, _ := newTestClient(router)
	decoratee, _ := newTestClient(router)
	caller, _ := newTestClient(router)
	ctx := context.Background()
	done := make(chan bool, 1)

	// test proprocess decorator: return nothing
	handlerFooBar := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		done <- true
		return &client.InvokeResult{}
	}

	decoratorHandler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		t.Log("Running decoratorHandler")
		// returning nothing: the call should be processed as usual
		return &client.InvokeResult{}
		// return nil
	}

	// Register old target URI
	if err := callee.Register("foo.test.bar.nothing", handlerFooBar, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Register handler URI
	if err := decoratee.Register("decoratortest.handlerURI.nothing", decoratorHandler, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Add preprocess decorator
	args := wamp.List{
		"preprocess",
		"foo.test.bar.nothing",
		"exact",
		"decoratortest.handlerURI.nothing",
		0,
		"async"}
	if _, err := decoratee.Call(ctx, "wamp.decorator.add", nil, args, nil, ""); err != nil {
		t.Fatalf("Error calling wamp.decorator.add: %v\n", err)
	}

	// Trigger preprocess decorator
	rsp, err := caller.Call(ctx, "foo.test.bar.nothing", nil, nil, nil, "")
	if err != nil {
		t.Fatalf("Error calling preprocess decorator: %v\n", err)
	}
	rspp, _ := json.MarshalIndent(rsp, "", "  ")
	t.Logf("rsp is: %s\n", rspp)
	go func() {
		time.Sleep(5 * time.Second)
		done <- false
	}()
	if !<-done {
		t.Fatal("Preprocess Decorator: HandlerFooBar not called after 5 seconds!")
	}
}

func TestPreProcessDecoratorWildcard(t *testing.T) {
	router, _ := newTestRouter()
	callee, _ := newTestClient(router)
	decoratee, _ := newTestClient(router)
	caller, _ := newTestClient(router)
	ctx := context.Background()
	done := make(chan bool, 1)

	handlerFooBar := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		return &client.InvokeResult{}
	}

	decoratorHandler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		t.Log("Running decoratorHandler")
		done <- true
		// returning nothing: the call should be processed as usual
		return &client.InvokeResult{}
		// return nil
	}

	// Register target URI
	if err := callee.Register("foo.test.bar.nothing", handlerFooBar, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Register handler URI
	if err := decoratee.Register("decoratortest.handlerURI.nothing", decoratorHandler, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Add preprocess decorator
	args := wamp.List{
		"preprocess",
		"foo.test..nothing",
		"wildcard",
		"decoratortest.handlerURI.nothing",
		0,
		"sync"}
	if _, err := decoratee.Call(ctx, "wamp.decorator.add", nil, args, nil, ""); err != nil {
		t.Fatalf("Error calling wamp.decorator.add: %v\n", err)
	}

	// Trigger preprocess decorator
	rsp, err := caller.Call(ctx, "foo.test.bar.nothing", nil, nil, nil, "")
	if err != nil {
		t.Fatalf("Error calling preprocess decorator: %v\n", err)
	}
	rspp, _ := json.MarshalIndent(rsp, "", "  ")
	t.Logf("rsp is: %s\n", rspp)
	go func() {
		time.Sleep(5 * time.Second)
		done <- false
	}()
	if !<-done {
		t.Fatal("Preprocess Decorator: Decorator not called after 5 seconds!")
	}
}

func TestPreProcessDecoratorPrefix(t *testing.T) {
	router, _ := newTestRouter()
	callee, _ := newTestClient(router)
	decoratee, _ := newTestClient(router)
	caller, _ := newTestClient(router)
	ctx := context.Background()
	done := make(chan bool, 1)

	handlerFooBar := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		return &client.InvokeResult{}
	}

	decoratorHandler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		t.Log("Running decoratorHandler")
		done <- true
		// returning nothing: the call should be processed as usual
		return &client.InvokeResult{}
		// return nil
	}

	// Register target URI
	if err := callee.Register("foo.test.bar.nothing", handlerFooBar, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Register handler URI
	if err := decoratee.Register("decoratortest.handlerURI.nothing", decoratorHandler, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Add preprocess decorator
	args := wamp.List{
		"preprocess",
		"foo.test",
		"prefix",
		"decoratortest.handlerURI.nothing",
		0,
		"sync"}
	if _, err := decoratee.Call(ctx, "wamp.decorator.add", nil, args, nil, ""); err != nil {
		t.Fatalf("Error calling wamp.decorator.add: %v\n", err)
	}

	// Trigger preprocess decorator
	rsp, err := caller.Call(ctx, "foo.test.bar.nothing", nil, nil, nil, "")
	if err != nil {
		t.Fatalf("Error calling preprocess decorator: %v\n", err)
	}
	rspp, _ := json.MarshalIndent(rsp, "", "  ")
	t.Logf("rsp is: %s\n", rspp)
	go func() {
		time.Sleep(5 * time.Second)
		done <- false
	}()
	if !<-done {
		t.Fatal("Preprocess Decorator: Decorator not called after 5 seconds!")
	}
}

func TestPreProcessDecoratorCall(t *testing.T) {
	router, _ := newTestRouter()
	callee, _ := newTestClient(router)
	decoratee, _ := newTestClient(router)
	caller, _ := newTestClient(router)
	ctx := context.Background()
	done := make(chan bool, 1)

	handlerFooBar := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		return &client.InvokeResult{
			Args: wamp.List{details},
		}
	}

	decoratorHandler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		t.Log("Running decoratorHandler")
		returnInv := serialize.WampMessageToList(&wamp.Invocation{
			Request:      args[0].(*wamp.Invocation).Request,
			Registration: wamp.GlobalID(),
			Details:      wamp.Dict{"Test": true},
			Arguments:    wamp.List{},
			ArgumentsKw:  wamp.Dict{},
		})
		done <- true
		return &client.InvokeResult{
			Args: wamp.List{returnInv},
		}
	}

	// Register target URI
	if err := callee.Register("foo.test.bar.nothing", handlerFooBar, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Register handler URI
	if err := decoratee.Register("decoratortest.handlerURI.nothing", decoratorHandler, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Add precall decorator
	args := wamp.List{
		"precall",
		"foo.test.bar.nothing",
		"exact",
		"decoratortest.handlerURI.nothing",
		0,
		"sync"}
	if _, err := decoratee.Call(ctx, "wamp.decorator.add", nil, args, nil, ""); err != nil {
		t.Fatalf("Error calling wamp.decorator.add: %v\n", err)
	}

	// Trigger precall decorator
	rsp, err := caller.Call(ctx, "foo.test.bar.nothing", nil, nil, nil, "")
	if err != nil {
		t.Fatalf("Error calling preprocess decorator: %v\n", err)
	}
	dict, ok := wamp.AsDict(rsp.Arguments[0])
	if !ok || dict["Test"] != true {
		t.Fatalf("Error changing Invocation Message!")
	}
	rspp, _ := json.MarshalIndent(rsp, "", "  ")
	t.Logf("rsp is: %s\n", rspp)
	go func() {
		time.Sleep(5 * time.Second)
		done <- false
	}()
	if !<-done {
		t.Fatal("Preprocess Decorator: Decorator not called after 5 seconds!")
	}
}

func TestPreProcessDecoratorCallMod(t *testing.T) {
	router, _ := newTestRouter()
	callee, _ := newTestClient(router)
	decoratee, _ := newTestClient(router)
	caller, _ := newTestClient(router)
	ctx := context.Background()
	done := make(chan bool, 1)

	handlerFooBar := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		return &client.InvokeResult{
			Args: wamp.List{details},
		}
	}

	decoratorHandler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		t.Log("Running decoratorHandler")
		returnInv := serialize.WampMessageToList(&wamp.Invocation{
			Request:      args[0].(*wamp.Invocation).Request,
			Registration: wamp.GlobalID(),
			Details:      wamp.Dict{"Test": true},
			Arguments:    wamp.List{},
			ArgumentsKw:  wamp.Dict{},
		})
		done <- true
		return &client.InvokeResult{
			Args: wamp.List{returnInv},
		}
	}

	// Register target URI
	if err := callee.Register("foo.test.bar.nothing", handlerFooBar, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Register handler URI
	if err := decoratee.Register("decoratortest.handlerURI.nothing", decoratorHandler, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Add precall decorator
	args := wamp.List{
		"precall",
		"foo.test.bar.nothing",
		"exact",
		"decoratortest.handlerURI.nothing",
		0,
		"sync"}
	if _, err := decoratee.Call(ctx, "wamp.decorator.add", nil, args, nil, ""); err != nil {
		t.Fatalf("Error calling wamp.decorator.add: %v\n", err)
	}

	// Trigger precall decorator
	rsp, err := caller.Call(ctx, "foo.test.bar.nothing", nil, nil, nil, "")
	if err != nil {
		t.Fatalf("Error calling preprocess decorator: %v\n", err)
	}
	dict, ok := wamp.AsDict(rsp.Arguments[0])
	if !ok || dict["Test"] != true {
		t.Fatalf("Error changing Invocation Message!")
	}
	rspp, _ := json.MarshalIndent(rsp, "", "  ")
	t.Logf("rsp is: %s\n", rspp)
	go func() {
		time.Sleep(5 * time.Second)
		done <- false
	}()
	if !<-done {
		t.Fatal("Preprocess Decorator: Decorator not called after 5 seconds!")
	}
}

func TestPreProcessDecoratorCallAsync(t *testing.T) {
	router, _ := newTestRouter()
	callee, _ := newTestClient(router)
	decoratee, _ := newTestClient(router)
	caller, _ := newTestClient(router)
	ctx := context.Background()
	done := make(chan bool, 1)

	handlerFooBar := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		return &client.InvokeResult{}
	}

	decoratorHandler := func(ctx context.Context, args wamp.List, kwargs, details wamp.Dict) *client.InvokeResult {
		t.Log("Running decoratorHandler")
		done <- true
		return &client.InvokeResult{}
	}

	// Register target URI
	if err := callee.Register("foo.test.bar.nothing", handlerFooBar, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Register handler URI
	if err := decoratee.Register("decoratortest.handlerURI.nothing", decoratorHandler, nil); err != nil {
		t.Fatalf("failed to register procedure: %v\n", err)
	}

	// Add precall decorator
	args := wamp.List{
		"precall",
		"foo.test.bar.nothing",
		"exact",
		"decoratortest.handlerURI.nothing",
		0,
		"async"}
	if _, err := decoratee.Call(ctx, "wamp.decorator.add", nil, args, nil, ""); err != nil {
		t.Fatalf("Error calling wamp.decorator.add: %v\n", err)
	}

	// Trigger precall decorator
	rsp, err := caller.Call(ctx, "foo.test.bar.nothing", nil, nil, nil, "")
	if err != nil {
		t.Fatalf("Error calling preprocess decorator: %v\n", err)
	}
	rspp, _ := json.MarshalIndent(rsp, "", "  ")
	t.Logf("rsp is: %s\n", rspp)
	go func() {
		time.Sleep(5 * time.Second)
		done <- false
	}()
	if !<-done {
		t.Fatal("Preprocess Decorator: Decorator not called after 5 seconds!")
	}
}
