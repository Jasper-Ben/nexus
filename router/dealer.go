package router

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/gammazero/nexus/stdlog"
	"github.com/gammazero/nexus/transport/serialize"
	"github.com/gammazero/nexus/wamp"
)

const (
	roleCallee = "callee"
	roleCaller = "caller"

	featureCallCanceling    = "call_canceling"
	featureCallDecoration   = "call_decoration"
	featureCallTimeout      = "call_timeout"
	featureCallerIdent      = "caller_identification"
	featurePatternBasedReg  = "pattern_based_registration"
	featureProgCallResults  = "progressive_call_results"
	featureSessionMetaAPI   = "session_meta_api"
	featureSharedReg        = "shared_registration"
	featureRegMetaAPI       = "registration_meta_api"
	featureTestamentMetaAPI = "testament_meta_api"
)

// Role information for this broker.
var dealerRole = wamp.Dict{
	"features": wamp.Dict{
		featureCallCanceling:    true,
		featureCallDecoration:   true,
		featureCallTimeout:      true,
		featureCallerIdent:      true,
		featurePatternBasedReg:  true,
		featureProgCallResults:  true,
		featureSessionMetaAPI:   true,
		featureSharedReg:        true,
		featureRegMetaAPI:       true,
		featureTestamentMetaAPI: true,
	},
}

type callState uint

const (
	callStatePreprocessDecorators callState = iota
	callStateProcessing
	callStatePrecallDecorators
	callStateResult
)

// registration holds the details of one or more registered remote procedure
type registration struct {
	id         wamp.ID  // registration ID
	procedure  wamp.URI // procedure this registration is for
	created    string   // when registration was created
	match      string   // how procedure uri is matched to registration
	policy     string   // how callee is selected if shared registration
	disclose   bool     // callee requests disclosure of caller identity
	nextCallee int      // choose callee for round-robin invocation.

	// Multiple sessions can register as callees depending on invocation policy
	// resulting in multiple procedures for the same registration ID.
	callees []*session
}

// call tracks all required properties for an invocation
type call struct {
	// The caller, i.e. the session which has originally initiated the call.
	caller *session
	// Request field in the original Call message
	callerRequestID wamp.ID
	originalCall    *wamp.Call

	// globally unique call ID
	callID wamp.ID

	currentState callState
	// The callee, i.e. the session the current processing step is routed to.
	currentCallee *session
	// The target callee, i.e. the session the call ultimately will be routed to.
	targetCallee *session
	// More callees which are currently in-flight, but their results are unused.
	// This list is used for async decorators, because their results should be discarded.
	additionalCallees map[wamp.ID]bool

	// Whether this call was canceled in-flight
	canceled bool
	// Whether to ignore the result, used for async decorators
	dropResult bool

	// The call message which might be changed due to decorator calls.
	// TBD: Evaluate whether we want to keep the original call message.
	currentCallMessage *wamp.Call
	// The invocation message which might be changed due to decorator calls.
	// TBD: Evaluate whether we want to keep the original invocation message.
	currentInvocationMessage *wamp.Invocation

	preProcessDecorators []*Decorator
	preCallDecorators    []*Decorator
	postCallDecorators   []*Decorator
}

type requestID struct {
	session wamp.ID
	request wamp.ID
}

type Dealer struct {
	// procedure URI -> registration ID
	procRegMap    map[wamp.URI]*registration
	pfxProcRegMap map[wamp.URI]*registration
	wcProcRegMap  map[wamp.URI]*registration

	preprocessDecorators *decoratorMap
	precallDecorators    *decoratorMap
	postcallDecorators   *decoratorMap

	// registration ID -> registration
	// Used to lookup registration by ID, needed for unregister.
	registrations map[wamp.ID]*registration

	// invocation ID -> call state
	invocations map[wamp.ID]*call

	// call ID -> invocation ID (for cancel, preprocess decorators)
	invocationByCall map[requestID]wamp.ID

	// callee session -> registration ID set.
	// Used to lookup registrations when removing a callee session.
	calleeRegIDSet map[*session]map[wamp.ID]struct{}

	actionChan chan func()

	// Generate registration IDs.
	idGen *wamp.IDGen

	// Used for round-robin call invocation.
	prng *rand.Rand

	// Dealer behavior flags.
	strictURI     bool
	allowDisclose bool

	metaPeer wamp.Peer

	// Meta-procedure registration ID -> handler func.
	metaProcMap map[wamp.ID]func(*wamp.Invocation) wamp.Message

	log   stdlog.StdLog
	debug bool
}

// NewDealer creates the default Dealer implementation.
//
// Messages are routed serially by the dealer's message handling goroutine.
// This serialization is limited to the work of determining the message's
// destination, and then the message is handed off to the next goroutine,
// typically the receiving client's send handler.
func NewDealer(logger stdlog.StdLog, strictURI, allowDisclose, debug bool) *Dealer {
	d := &Dealer{
		procRegMap:    map[wamp.URI]*registration{},
		pfxProcRegMap: map[wamp.URI]*registration{},
		wcProcRegMap:  map[wamp.URI]*registration{},

		preprocessDecorators: newDecoratorMap(),
		precallDecorators:    newDecoratorMap(),
		postcallDecorators:   newDecoratorMap(),

		registrations:  map[wamp.ID]*registration{},
		calleeRegIDSet: map[*session]map[wamp.ID]struct{}{},

		invocationByCall: map[requestID]wamp.ID{},
		invocations:      map[wamp.ID]*call{},

		// The action handler should be nearly always runable, since it is the
		// critical section that does the only routing.  So, and unbuffered
		// channel is appropriate.
		actionChan: make(chan func()),

		idGen: new(wamp.IDGen),
		prng:  rand.New(rand.NewSource(time.Now().Unix())),

		strictURI:     strictURI,
		allowDisclose: allowDisclose,

		log:   logger,
		debug: debug,
	}
	go d.run()
	return d
}

// SetMetaPeer sets the client that the dealer uses to publish meta events.
func (d *Dealer) SetMetaPeer(metaPeer wamp.Peer) {
	d.actionChan <- func() {
		d.metaPeer = metaPeer
	}
}

// Role returns the role information for the "dealer" role.  The data returned
// is suitable for use as broker role info in a WELCOME message.
func (d *Dealer) Role() wamp.Dict {
	return dealerRole
}

// Register registers a callee to handle calls to a procedure.
//
// If the shared_registration feature is supported, and if allowed by the
// invocation policy, multiple callees may register to handle the same
// procedure.
func (d *Dealer) Register(callee *session, msg *wamp.Register) {
	if callee == nil || msg == nil {
		panic("dealer.Register with nil session or message")
	}

	// Validate procedure URI.  For REGISTER, must be valid URI (either strict
	// or loose), and all URI components must be non-empty other than for
	// wildcard or prefix matched procedures.
	match, _ := wamp.AsString(msg.Options[wamp.OptMatch])
	if !msg.Procedure.ValidURI(d.strictURI, match) {
		errMsg := fmt.Sprintf(
			"register for invalid procedure URI %v (URI strict checking %v)",
			msg.Procedure, d.strictURI)
		d.trySend(callee, &wamp.Error{
			Type:      msg.MessageType(),
			Request:   msg.Request,
			Error:     wamp.ErrInvalidURI,
			Arguments: wamp.List{errMsg},
		})
		return
	}

	wampURI := strings.HasPrefix(string(msg.Procedure), "wamp.")

	// Disallow registration of procedures starting with "wamp." by sessions
	// other then the meta session.
	if wampURI && callee.ID != metaID {
		errMsg := fmt.Sprintf("register for restricted procedure URI %v",
			msg.Procedure)
		d.trySend(callee, &wamp.Error{
			Type:      msg.MessageType(),
			Request:   msg.Request,
			Error:     wamp.ErrInvalidURI,
			Arguments: wamp.List{errMsg},
		})
		return
	}

	// If callee requests disclosure of caller identity, but dealer does not
	// allow, then send error as registration response.
	disclose, _ := msg.Options[wamp.OptDiscloseCaller].(bool)
	// allow disclose for trusted clients
	if !d.allowDisclose && disclose {
		callee.rLock()
		authrole, _ := wamp.AsString(callee.Details["authrole"])
		callee.rUnlock()
		if authrole != "trusted" {
			d.trySend(callee, &wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrOptionDisallowedDiscloseMe,
			})
			return
		}
	}

	// If the callee supports progressive call results, but does not support
	// call canceling, then disable the callee's progressive call results
	// feature.  Call canceling is necessary to stop progressive results if
	// the caller session is closed during progressive result delivery.
	if callee.HasFeature(roleCallee, featureProgCallResults) {
		if !callee.HasFeature(roleCallee, featureCallCanceling) {
			dict := wamp.DictChild(callee.Details, "roles")
			dict = wamp.DictChild(dict, roleCallee)
			dict = wamp.DictChild(dict, "features")
			delete(dict, featureProgCallResults)
			d.log.Println("disabling", featureProgCallResults, "for callee",
				callee, "that does not support", featureCallCanceling)
		}
	}

	invoke, _ := wamp.AsString(msg.Options[wamp.OptInvoke])
	d.actionChan <- func() {
		d.register(callee, msg, match, invoke, disclose, wampURI)
	}
}

// Unregister removes a remote procedure previously registered by the callee.
func (d *Dealer) Unregister(callee *session, msg *wamp.Unregister) {
	if callee == nil || msg == nil {
		panic("dealer.Unregister with nil session or message")
	}
	d.actionChan <- func() {
		d.unregister(callee, msg)
	}
}

// Call invokes a registered remote procedure.
func (d *Dealer) Call(caller *session, msg *wamp.Call) {
	if caller == nil || msg == nil {
		panic("dealer.Call with nil session or message")
	}
	d.actionChan <- func() {
		d.call(caller, msg, wamp.GlobalID())
	}
}

// Cancel actively cancels a call that is in progress.
//
// Cancellation behaves differently depending on the mode:
//
// "skip": The pending call is canceled and ERROR is send immediately back to
// the caller.  No INTERRUPT is sent to the callee and the result is discarded
// when received.
//
// "kill": INTERRUPT is sent to the client, but ERROR is not returned to the
// caller until after the callee has responded to the canceled call.  In this
// case the caller may receive RESULT or ERROR depending whether the callee
// finishes processing the invocation or the interrupt first.
//
// "killnowait": The pending call is canceled and ERROR is send immediately
// back to the caller.  INTERRUPT is sent to the callee and any response to the
// invocation or interrupt from the callee is discarded when received.
//
// If the callee does not support call canceling, then behavior is "skip".
func (d *Dealer) Cancel(caller *session, msg *wamp.Cancel) {
	if caller == nil || msg == nil {
		panic("dealer.Cancel with nil session or message")
	}
	// Cancel mode should be one of: "skip", "kill", "killnowait"
	mode, _ := wamp.AsString(msg.Options[wamp.OptMode])
	switch mode {
	case wamp.CancelModeKillNoWait, wamp.CancelModeKill, wamp.CancelModeSkip:
	case "":
		mode = wamp.CancelModeKillNoWait
	default:
		d.trySend(caller, &wamp.Error{
			Type:      msg.MessageType(),
			Request:   msg.Request,
			Error:     wamp.ErrInvalidArgument,
			Arguments: wamp.List{fmt.Sprint("invalid cancel mode ", mode)},
		})
		return
	}

	d.actionChan <- func() {
		d.cancel(caller, msg, mode, wamp.ErrCanceled)
	}
}

// Yield handles the result of successfully processing and finishing the
// execution of a call, send from callee to dealer.
func (d *Dealer) Yield(callee *session, msg *wamp.Yield) {
	if callee == nil || msg == nil {
		panic("dealer.Yield with nil session or message")
	}
	d.actionChan <- func() {
		d.yield(callee, msg)
	}
}

// Error handles an invocation error returned by the callee.
func (d *Dealer) Error(origin *session, msg *wamp.Error) {
	if msg == nil {
		panic("dealer.Error with nil message")
	}
	d.actionChan <- func() {
		d.error(origin, msg)
	}
}

// RemoveSession is called when a client leaves the
// realm by sending a GOODBYE message or by disconnecting from the router.  If
// there are any registrations for this session wamp.registration.on_unregister
// and wamp.registration.on_delete meta events are published for each.
func (d *Dealer) RemoveSession(sess *session) {
	if sess == nil {
		// No session specified, no session removed.
		return
	}
	d.actionChan <- func() {
		d.removeSession(sess)
	}
}

// Close stops the dealer, letting already queued actions finish.
func (d *Dealer) Close() {
	close(d.actionChan)
}

func (d *Dealer) run() {
	for action := range d.actionChan {
		action()
	}
	if d.debug {
		d.log.Print("Dealer stopped")
	}
}

func (d *Dealer) register(callee *session, msg *wamp.Register, match, invokePolicy string, disclose, wampURI bool) {
	var reg *registration
	switch match {
	default:
		reg = d.procRegMap[msg.Procedure]
	case wamp.MatchPrefix:
		reg = d.pfxProcRegMap[msg.Procedure]
	case wamp.MatchWildcard:
		reg = d.wcProcRegMap[msg.Procedure]
	}

	var created string
	var regID wamp.ID
	// If no existing registration found for the procedure, then create a new
	// registration.
	if reg == nil {
		regID = d.idGen.Next()
		created = wamp.NowISO8601()
		reg = &registration{
			id:        regID,
			procedure: msg.Procedure,
			created:   created,
			match:     match,
			policy:    invokePolicy,
			disclose:  disclose,
			callees:   []*session{callee},
		}
		d.registrations[regID] = reg
		switch match {
		default:
			d.procRegMap[msg.Procedure] = reg
		case wamp.MatchPrefix:
			d.pfxProcRegMap[msg.Procedure] = reg
		case wamp.MatchWildcard:
			d.wcProcRegMap[msg.Procedure] = reg
		}

		if !wampURI && d.metaPeer != nil {
			// wamp.registration.on_create is fired when a registration is
			// created through a registration request for an URI which was
			// previously without a registration.
			details := wamp.Dict{
				"id":           regID,
				"created":      created,
				"uri":          msg.Procedure,
				wamp.OptMatch:  match,
				wamp.OptInvoke: invokePolicy,
			}
			d.metaPeer.Send(&wamp.Publish{
				Request:   wamp.GlobalID(),
				Topic:     wamp.MetaEventRegOnCreate,
				Arguments: wamp.List{callee.ID, details},
			})
		}
	} else {
		// There is an existing registration(s) for this procedure.  See if
		// invocation policy allows another.

		// Found an existing registration that has an invocation strategy that
		// only allows a single callee on a the given registration.
		if reg.policy == "" || reg.policy == wamp.InvokeSingle {
			d.log.Println("REGISTER for already registered procedure",
				msg.Procedure, "from callee", callee)
			d.trySend(callee, &wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrProcedureAlreadyExists,
			})
			return
		}

		// Found an existing registration that has an invocation strategy
		// different from the one requested by the new callee
		if reg.policy != invokePolicy {
			d.log.Println("REGISTER for already registered procedure",
				msg.Procedure, "with conflicting invocation policy (has",
				reg.policy, "and", invokePolicy, "was requested")
			d.trySend(callee, &wamp.Error{
				Type:    msg.MessageType(),
				Request: msg.Request,
				Details: wamp.Dict{},
				Error:   wamp.ErrProcedureAlreadyExists,
			})
			return
		}

		regID = reg.id

		// Add callee for the registration.
		reg.callees = append(reg.callees, callee)
	}

	// Add the registration ID to the callees set of registrations.
	if _, ok := d.calleeRegIDSet[callee]; !ok {
		d.calleeRegIDSet[callee] = map[wamp.ID]struct{}{}
	}
	d.calleeRegIDSet[callee][regID] = struct{}{}

	if d.debug {
		d.log.Printf("Registered procedure %v (regID=%v) to callee %v",
			msg.Procedure, regID, callee)
	}
	d.trySend(callee, &wamp.Registered{
		Request:      msg.Request,
		Registration: regID,
	})

	if !wampURI && d.metaPeer != nil {
		// Publish wamp.registration.on_register meta event.  Fired when a
		// session is added to a registration.  A wamp.registration.on_register
		// event MUST be fired subsequent to a wamp.registration.on_create
		// event, since the first registration results in both the creation of
		// the registration and the addition of a session.
		d.metaPeer.Send(&wamp.Publish{
			Request:   wamp.GlobalID(),
			Topic:     wamp.MetaEventRegOnRegister,
			Arguments: wamp.List{callee.ID, regID},
		})
	}
}

func (d *Dealer) unregister(callee *session, msg *wamp.Unregister) {
	// Delete the registration ID from the callee's set of registrations.
	if _, ok := d.calleeRegIDSet[callee]; ok {
		delete(d.calleeRegIDSet[callee], msg.Registration)
		if len(d.calleeRegIDSet[callee]) == 0 {
			delete(d.calleeRegIDSet, callee)
		}
	}

	delReg, err := d.delCalleeReg(callee, msg.Registration)
	if err != nil {
		d.log.Println("Cannot unregister:", err)
		d.trySend(callee, &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: wamp.Dict{},
			Error:   wamp.ErrNoSuchRegistration,
		})
		return
	}

	d.trySend(callee, &wamp.Unregistered{Request: msg.Request})

	if d.metaPeer == nil {
		return
	}

	// Publish wamp.registration.on_unregister meta event.  Fired when a
	// session is removed from a subscription.
	d.metaPeer.Send(&wamp.Publish{
		Request:   wamp.GlobalID(),
		Topic:     wamp.MetaEventRegOnUnregister,
		Arguments: wamp.List{callee.ID, msg.Registration},
	})

	if delReg {
		// Publish wamp.registration.on_delete meta event.  Fired when a
		// registration is deleted after the last session attached to it has
		// been removed.  The wamp.registration.on_delete event MUST be
		// preceded by a wamp.registration.on_unregister event.
		d.metaPeer.Send(&wamp.Publish{
			Request:   wamp.GlobalID(),
			Topic:     wamp.MetaEventRegOnDelete,
			Arguments: wamp.List{callee.ID, msg.Registration},
		})
	}
}

// matchProcedure finds the best matching registration given a procedure URI.
//
// If there are both matching prefix and wildcard registrations, then find the
// one with the more specific match (longest matched pattern).
func (d *Dealer) matchProcedure(procedure wamp.URI) (*registration, bool) {
	// Find registered procedures with exact match.
	reg, ok := d.procRegMap[procedure]
	if !ok {
		// No exact match was found.  So, search for a prefix or wildcard
		// match, and prefer the most specific math (longest matched pattern).
		// If there is a tie, then prefer the first longest prefix.
		matchCount := -1 // initialize matchCount to -1 to catch an empty registration.
		for pfxProc, pfxReg := range d.pfxProcRegMap {
			if procedure.PrefixMatch(pfxProc) {
				if len(pfxProc) > matchCount {
					reg = pfxReg
					matchCount = len(pfxProc)
					ok = true
				}
			}
		}
		// according to the spec, we have to prefer prefix match over wildcard match:
		// https://wamp-proto.org/static/rfc/draft-oberstet-hybi-crossbar-wamp.html#rfc.section.14.3.8.1.4.2
		if ok {
			return reg, ok
		}

		for wcProc, wcReg := range d.wcProcRegMap {
			if procedure.WildcardMatch(wcProc) {
				if len(wcProc) > matchCount {
					reg = wcReg
					matchCount = len(wcProc)
					ok = true
				}
			}
		}
	}
	return reg, ok
}

func (d *Dealer) buildInvocation(callObj *call, msg *wamp.Call, procedure wamp.URI, isDecorator bool, isSync bool) (*session, *wamp.Invocation, *wamp.Error) {
	reg, ok := d.matchProcedure(procedure)
	if !ok || len(reg.callees) == 0 {
		// If no registered procedure, send error.
		return nil, nil, &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: wamp.Dict{},
			Error:   wamp.ErrNoSuchProcedure,
		}
	}

	var callee *session

	// If there are multiple callees, then select a callee based invocation
	// policy.
	if len(reg.callees) > 1 {
		switch reg.policy {
		case wamp.InvokeFirst:
			callee = reg.callees[0]
		case wamp.InvokeRoundRobin:
			if reg.nextCallee >= len(reg.callees) {
				reg.nextCallee = 0
			}
			callee = reg.callees[reg.nextCallee]
			reg.nextCallee++
		case wamp.InvokeRandom:
			callee = reg.callees[d.prng.Int63n(int64(len(reg.callees)))]
		case wamp.InvokeLast:
			callee = reg.callees[len(reg.callees)-1]
		default:
			errMsg := fmt.Sprint("multiple callees registered for ",
				msg.Procedure, " with '", wamp.InvokeSingle, "' policy")
			// This is disallowed by the dealer, and is a programming error if
			// it ever happened, so panic.
			panic(errMsg)
		}
	} else {
		callee = reg.callees[0]
	}
	details := wamp.Dict{}

	// A Caller might want to issue a call providing a timeout for the call to
	// finish.
	//
	// A timeout allows to automatically cancel a call after a specified time
	// either at the Callee or at the Dealer.
	timeout, _ := wamp.AsInt64(msg.Options[wamp.OptTimeout])
	if timeout > 0 {
		// Check that callee supports call_timeout.
		if callee.HasFeature(roleCallee, featureCallTimeout) {
			details[wamp.OptTimeout] = timeout
		}

		// TODO: Start a goroutine to cancel the pending call on timeout.
		// Should be implemented like Cancel with mode=killnowait, and error
		// message argument should say "call timeout"
	}

	// TODO: handle trust levels

	// If the callee has requested disclosure of caller identity when the
	// registration was created, and this was allowed by the dealer.
	if reg.disclose {
		if callee.ID == metaID {
			details[roleCaller] = callObj.caller.ID
		}
		discloseCaller(callObj.caller, details)
	} else {
		// A Caller MAY request the disclosure of its identity (its WAMP
		// session ID) to endpoints of a routed call.  This is indicated by the
		// "disclose_me" flag in the message options.
		if opt, _ := msg.Options[wamp.OptDiscloseMe].(bool); opt {
			// Dealer MAY deny a Caller's request to disclose its identity.
			if !d.allowDisclose {
				return nil, nil, &wamp.Error{
					Type:    msg.MessageType(),
					Request: msg.Request,
					Details: wamp.Dict{},
					Error:   wamp.ErrOptionDisallowedDiscloseMe,
				}
			}
			if callee.HasFeature(roleCallee, featureCallerIdent) {
				discloseCaller(callObj.caller, details)
			}
		}
	}

	// A Caller indicates its willingness to receive progressive results by
	// setting CALL.Options.receive_progress|bool := true
	if opt, _ := msg.Options[wamp.OptReceiveProgress].(bool); opt {
		// If the Callee supports progressive calls, the Dealer will
		// forward the Caller's willingness to receive progressive
		// results by setting.
		if callee.HasFeature(roleCallee, featureProgCallResults) {
			details[wamp.OptReceiveProgress] = true
		}
	}

	if reg.match != wamp.MatchExact {
		// According to the spec, a router must provide the actual
		// procedure to the client.
		// We also want to provide the procedure to decorator calls.
		details[wamp.OptProcedure] = procedure
	}
	if isDecorator {
		details[wamp.OptDecoratedProcedure] = msg.Procedure
	}
	requestID := callObj.callID
	if !isSync {
		requestID = wamp.GlobalID()
	}
	return callee, &wamp.Invocation{
		Request:      requestID,
		Registration: reg.id,
		Details:      details,
		Arguments:    msg.Arguments,
		ArgumentsKw:  msg.ArgumentsKw,
	}, nil
}

func (d *Dealer) callInt(callObj *call, msg *wamp.Call, procedure wamp.URI, isDecorator bool, isSync bool) *wamp.Error {
	// perform the router processing, doing some error checks etc.
	callee, invocation, err := d.buildInvocation(callObj, msg, procedure, isDecorator, isSync)
	if err != nil {
		return err
	}
	// Send INVOCATION to the endpoint that has registered the requested
	// procedure.
	if !d.trySend(callee, invocation) {
		return &wamp.Error{
			Type:      msg.MessageType(),
			Request:   msg.Request,
			Details:   wamp.Dict{},
			Error:     wamp.ErrNetworkFailure,
			Arguments: wamp.List{"client blocked - cannot call procedure"},
		}
	}
	if isSync {
		// store the callee we just sent the invocation to
		callObj.currentCallee = callee
	} else {
		callObj := &call{
			dropResult: true,
		}
		d.invocations[invocation.Request] = callObj
	}
	return nil
}

func (d *Dealer) call(caller *session, msg *wamp.Call, uniqueID wamp.ID) {
	callObj, ok := d.invocations[uniqueID]
	if !ok {
		// There is no call registered with this call ID, therefore `d.call` has been
		// invoked by a client and we need to resolve the preprocess decorators and
		// store them into the call list.
		decorators := d.preprocessDecorators.matchDecorators(msg.Procedure)
		callState := callStateProcessing
		// Basically, we have two cases:
		if len(decorators) > 0 {
			// 1. One or more decorators were found, in this case, start by invoking the first decorator
			//  by just constructing the CALL message and keep the reference to the old one around.
			callState = callStatePreprocessDecorators
		}

		// 2. No decorators were found, so we skip the preprocessDecorators state and move on to
		// the processing call state and putting a nil list into the call object.

		callObj = &call{
			caller:          caller,
			callerRequestID: msg.Request,
			originalCall:    msg,

			callID:                   uniqueID,
			currentState:             callState,
			currentCallee:            nil,
			additionalCallees:        nil,
			currentCallMessage:       msg,
			currentInvocationMessage: nil,
			preProcessDecorators:     decorators,
			preCallDecorators:        nil,
			postCallDecorators:       nil,

			canceled:   false,
			dropResult: false,
		}
		d.invocations[uniqueID] = callObj
		d.invocationByCall[requestID{
			session: caller.ID,
			request: msg.Request,
		}] = uniqueID
	}

	for callObj.currentState == callStatePreprocessDecorators && len(callObj.preProcessDecorators) > 0 {
		isSync := callObj.preProcessDecorators[0].callType == wamp.DecoratorCallTypeSync
		if err := d.callInt(callObj, &wamp.Call{
			Request:     callObj.callerRequestID,
			Procedure:   callObj.originalCall.Procedure,
			Options:     callObj.originalCall.Options,
			Arguments:   wamp.List{callObj.currentCallMessage},
			ArgumentsKw: wamp.Dict{},
		}, callObj.preProcessDecorators[0].handlerURI, true, isSync); err != nil {
			delete(d.invocations, uniqueID)
			delete(d.invocationByCall, requestID{session: callObj.caller.ID, request: callObj.callerRequestID})
			d.trySend(callObj.caller, err)
		} else {
			callObj.preProcessDecorators = callObj.preProcessDecorators[1:]
			if len(callObj.preProcessDecorators) == 0 {
				callObj.currentState = callStateProcessing
				if !isSync {
					// chain is empty, no more synchronous results to come, so move on to the processing
					break
				}
			}
		}
		if isSync {
			// await the first synchronous result and then move on.
			return
		}
	}
	if callObj.currentState == callStateProcessing {
		targetCallee, invocationMessage, errorMessage := d.buildInvocation(callObj, &wamp.Call{
			Request:     callObj.callerRequestID,
			Options:     callObj.currentCallMessage.Options,
			Arguments:   callObj.currentCallMessage.Arguments,
			ArgumentsKw: callObj.currentCallMessage.ArgumentsKw,
			Procedure:   callObj.currentCallMessage.Procedure,
		}, callObj.currentCallMessage.Procedure, false, true)
		if errorMessage != nil {
			delete(d.invocations, uniqueID)
			delete(d.invocationByCall, requestID{session: callObj.caller.ID, request: callObj.callerRequestID})
			d.trySend(callObj.caller, errorMessage)
			return
		}
		callObj.currentInvocationMessage = invocationMessage
		callObj.targetCallee = targetCallee
		callObj.preCallDecorators = d.precallDecorators.matchDecorators(callObj.currentCallMessage.Procedure)
		if len(callObj.preCallDecorators) > 0 {
			callObj.currentState = callStatePrecallDecorators
		} else {
			callObj.currentState = callStateResult
		}
	}
	for callObj.currentState == callStatePrecallDecorators && len(callObj.preCallDecorators) > 0 {
		isSync := callObj.preCallDecorators[0].callType == wamp.DecoratorCallTypeSync
		if err := d.callInt(callObj, &wamp.Call{
			Request:     callObj.callerRequestID,
			Procedure:   callObj.originalCall.Procedure,
			Options:     callObj.originalCall.Options,
			Arguments:   wamp.List{callObj.currentInvocationMessage},
			ArgumentsKw: wamp.Dict{},
		}, callObj.preCallDecorators[0].handlerURI, true, isSync); err != nil {
			delete(d.invocations, uniqueID)
			delete(d.invocationByCall, requestID{session: callObj.caller.ID, request: callObj.callerRequestID})
			d.trySend(callObj.caller, err)
		} else {
			callObj.preCallDecorators = callObj.preCallDecorators[1:]
			if len(callObj.preCallDecorators) == 0 {
				callObj.currentState = callStateResult
				if !isSync {
					// chain is empty, no more synchronous results to come, so move on to the processing
					break
				}
			}
		}
		if isSync {
			// await the first synchronous result and then move on.
			return
		}
	}

	// If we get here, it means that for the call request,
	// no decorator needs to be considered anymore and we can process the call.

	// Send INVOCATION to the endpoint that has registered the requested
	// procedure.
	if !d.trySend(callObj.targetCallee, callObj.currentInvocationMessage) {
		delete(d.invocations, uniqueID)
		delete(d.invocationByCall, requestID{session: callObj.caller.ID, request: callObj.callerRequestID})
		d.trySend(callObj.caller, &wamp.Error{
			Type:      msg.MessageType(),
			Request:   msg.Request,
			Details:   wamp.Dict{},
			Error:     wamp.ErrNetworkFailure,
			Arguments: wamp.List{"client blocked - cannot call procedure"},
		})
	}
	// store the callee we just sent the invocation to
	callObj.currentCallee = callObj.targetCallee
}

func (d *Dealer) cancel(caller *session, msg *wamp.Cancel, mode string, reason wamp.URI) {
	reqID := requestID{
		session: caller.ID,
		request: msg.Request,
	}
	callID, ok := d.invocationByCall[reqID]
	if !ok {
		d.log.Print("Found call with no pending invocation")
		// There is no pending call to cancel.
		return
	}
	callObj, ok := d.invocations[callID]
	if !ok {
		// Call object couldn't be found.
		d.log.Print("CRITICAL: missing caller for pending invocation")
		return
	}

	// Check if the caller of cancel is also the caller of the procedure.
	if caller != callObj.caller {
		// The caller it trying to cancel calls that it does not own.  It it
		// either confused or trying to do something bad.
		d.log.Println("CANCEL received from caller", caller,
			"for call owned by different session")
		return
	}
	// For those who repeatedly press elevator buttons.
	if callObj.canceled {
		return
	}
	callObj.canceled = true

	// If mode is "kill" or "killnowait", then send INTERRUPT.
	if mode != wamp.CancelModeSkip {
		// Check that callee supports call canceling to see if it is alright to
		// send INTERRUPT to callee.
		if !callObj.currentCallee.HasFeature(roleCallee, featureCallCanceling) {
			// Cancel in dealer without sending INTERRUPT to callee.
			d.log.Println("Callee", callObj.currentCallee, "does not support call canceling")
		} else {
			// Send INTERRUPT message to callee.
			if d.trySend(callObj.currentCallee, &wamp.Interrupt{
				Request: callObj.callID,
				Options: wamp.Dict{wamp.OptReason: reason, wamp.OptMode: mode},
			}) {
				d.log.Println("Dealer sent INTERRUPT to cancel invocation",
					callObj.callID, "for call", msg.Request, "mode:", mode)

				// If mode is "kill" then let error from callee trigger the
				// response to the caller.  This is how the caller waits for
				// the callee to cancel the call.
				if mode == wamp.CancelModeKill {
					return
				}
			}
		}
	}
	// Treat any unrecognized mode the same as "skip".

	// Immediately delete the pending call and send ERROR back to the
	// caller.  This will cause any RESULT or ERROR arriving later from the
	// callee to be dropped.
	//
	// This also stops repeated CANCEL messages.
	delete(d.invocationByCall, reqID)
	delete(d.invocations, callObj.callID)

	// Send error to the caller.
	d.trySend(caller, &wamp.Error{
		Type:    wamp.CALL,
		Request: msg.Request,
		Error:   reason,
		Details: wamp.Dict{},
	})
}

func getMessage(args wamp.List, msgKind wamp.MessageType) wamp.Message {
	if len(args) == 0 {
		return nil
	}
	list, ok := wamp.AsList(args[0])
	if !ok {
		return nil
	}
	cmsg, err := serialize.ListToWampMessage(msgKind, list)
	if err != nil {
		return nil
	}
	return cmsg
}

func (d *Dealer) yield(callee *session, msg *wamp.Yield) {
	progress, _ := msg.Options[wamp.OptProgress].(bool)

	// Find and delete pending invocation.
	callObj, ok := d.invocations[msg.Request]
	if !ok {
		// The pending invocation is gone, which means the caller has left the
		// realm or canceled the call.
		//
		// Send INTERRUPT to cancel progressive results.
		if progress {
			// It is alright to send an INTERRUPT to the callee, since the
			// callee's progressive call results feature would have been
			// disabled at registration time if the callee did not support call
			// canceling.
			if d.trySend(callee, &wamp.Interrupt{
				Request: msg.Request,
				Options: wamp.Dict{wamp.OptMode: wamp.CancelModeKillNoWait},
			}) {
				d.log.Println("Dealer sent INTERRUPT to cancel progressive",
					"results for request", msg.Request, "to callee", callee)
			}
		} else {
			// WAMP does not allow sending INTERRUPT in response to normal or
			// final YIELD message.
			d.log.Println("YIELD received with unknown invocation request ID:",
				msg.Request)
		}
		return
	}
	if callObj.dropResult {
		delete(d.invocations, msg.Request)
		return
	}

	if callObj.currentCallee.ID != callee.ID {
		d.log.Println("Dealer received YIELD from not-owned callee", callee, ", ignoring")
		return
	}
	if callObj.currentState != callStateResult && progress {
		d.log.Println("Dropping progressive result for preprocess or precall decorator, not supported")
		return
	}

	switch callObj.currentState {
	case callStatePreprocessDecorators:
		// preprocess decorator returned a result.
		callObj.currentCallee = nil
		decoratedCall := getMessage(msg.Arguments, wamp.CALL)
		if decoratedCall != nil {
			callObj.currentCallMessage = decoratedCall.(*wamp.Call)
		}
		d.call(callObj.caller, callObj.currentCallMessage, callObj.callID)
		return
	case callStatePrecallDecorators:
		callObj.currentCallee = nil
		decoratedInvocation := getMessage(msg.Arguments, wamp.INVOCATION)
		if decoratedInvocation != nil {
			callObj.currentInvocationMessage = decoratedInvocation.(*wamp.Invocation)
		}
		d.call(callObj.caller, callObj.currentCallMessage, callObj.callID)
		return
	}

	details := wamp.Dict{}

	if !progress {
		// Delete callID -> invocation.
		delete(d.invocations, callObj.callID)
		delete(d.invocationByCall, requestID{session: callObj.caller.ID, request: callObj.callerRequestID})
	} else {
		// If this is a progressive response, then set progress=true.
		details[wamp.OptProgress] = true
	}

	// Send RESULT to the caller.  This forwards the YIELD from the callee.
	d.trySend(callObj.caller, &wamp.Result{
		Request:     callObj.callerRequestID,
		Details:     details,
		Arguments:   msg.Arguments,
		ArgumentsKw: msg.ArgumentsKw,
	})
}

func (d *Dealer) error(origin *session, msg *wamp.Error) {
	// When we receive an error for an invocation it could be either from a decorator
	// or from the endpoint.
	// Which in both cases means that we want to forward the error to the client.
	// Find and delete pending invocation.
	callObj, ok := d.invocations[msg.Request]
	if !ok {
		d.log.Println("Received ERROR (INVOCATION) with invalid request ID:",
			msg.Request, "(response to canceled call)")
		return
	}

	if callObj.currentCallee.ID != origin.ID {
		d.log.Println("Dealer received ERROR from not-owned callee", origin, ", ignoring")
		return
	}

	// Delete invocationsByCall entry.  This will already be deleted if the
	// call canceled with mode "skip" or "killnowait".
	delete(d.invocations, callObj.callID)
	delete(d.invocationByCall, requestID{session: callObj.caller.ID, request: callObj.callerRequestID})

	// Send error to the caller.
	d.trySend(callObj.caller, &wamp.Error{
		Type:        wamp.CALL,
		Request:     callObj.callerRequestID,
		Error:       msg.Error,
		Details:     msg.Details,
		Arguments:   msg.Arguments,
		ArgumentsKw: msg.ArgumentsKw,
	})
}

func (d *Dealer) removeSession(sess *session) {
	// Remove any remaining registrations for the removed session.
	for regID := range d.calleeRegIDSet[sess] {
		delReg, err := d.delCalleeReg(sess, regID)
		if err != nil {
			panic("!!! Callee had ID of nonexistent registration")
		}

		if d.metaPeer == nil {
			continue
		}

		// Publish wamp.registration.on_unregister meta event.  Fired when a
		// callee session is removed from a registration.
		d.metaPeer.Send(&wamp.Publish{
			Request:   wamp.GlobalID(),
			Topic:     wamp.MetaEventRegOnUnregister,
			Arguments: wamp.List{sess.ID, regID},
		})

		if !delReg {
			continue
		}
		// Publish wamp.registration.on_delete meta event.  Fired when a
		// registration is deleted after the last session attached to it
		// has been removed.  The wamp.registration.on_delete event MUST be
		// preceded by a wamp.registration.on_unregister event.
		d.metaPeer.Send(&wamp.Publish{
			Request:   wamp.GlobalID(),
			Topic:     wamp.MetaEventRegOnDelete,
			Arguments: wamp.List{sess.ID, regID},
		})
	}
	delete(d.calleeRegIDSet, sess)
	for callID, callObj := range d.invocations {
		if callObj.currentCallee == sess {

		}
		if callObj.caller == sess || callObj.currentCallee == sess {
			delete(d.invocations, callID)
			delete(d.invocationByCall, requestID{session: callObj.caller.ID, request: callObj.callerRequestID})
		}
	}
}

// delCalleeReg deletes the the callee from the specified registration and
// deletes the registration from the set of registrations for the callee.
//
// If there are no more callees for the registration, then the registration is
// removed and true is returned to indicate that the last registration was
// deleted.
func (d *Dealer) delCalleeReg(callee *session, regID wamp.ID) (bool, error) {
	reg, ok := d.registrations[regID]
	if !ok {
		// The registration doesn't exist
		return false, fmt.Errorf("no such registration: %v", regID)
	}

	// Remove the callee from the registration.
	for i := range reg.callees {
		if reg.callees[i] == callee {
			if d.debug {
				d.log.Printf("Unregistered procedure %v (regID=%v) (callee=%v)",
					reg.procedure, regID, callee.ID)
			}
			if len(reg.callees) == 1 {
				reg.callees = nil
			} else {
				// Delete preserving order.
				reg.callees = append(reg.callees[:i], reg.callees[i+1:]...)
			}
			break
		}
	}

	// If no more callees for this registration, then delete the registration
	// according to what match type it is.
	if len(reg.callees) == 0 {
		delete(d.registrations, regID)
		switch reg.match {
		default:
			delete(d.procRegMap, reg.procedure)
		case wamp.MatchPrefix:
			delete(d.pfxProcRegMap, reg.procedure)
		case wamp.MatchWildcard:
			delete(d.wcProcRegMap, reg.procedure)
		}
		if d.debug {
			d.log.Printf("Deleted registration %v for procedure %v", regID,
				reg.procedure)
		}
		return true, nil
	}
	return false, nil
}

// ----- Meta Procedure Handlers -----

// RegList retrieves registration IDs listed according to match policies.
func (d *Dealer) RegList(msg *wamp.Invocation) wamp.Message {
	var exactRegs, pfxRegs, wcRegs []wamp.ID
	sync := make(chan struct{})
	d.actionChan <- func() {
		for _, reg := range d.procRegMap {
			exactRegs = append(exactRegs, reg.id)
		}
		for _, reg := range d.pfxProcRegMap {
			pfxRegs = append(pfxRegs, reg.id)
		}
		for _, reg := range d.wcProcRegMap {
			wcRegs = append(wcRegs, reg.id)
		}
		close(sync)
	}
	<-sync
	dict := wamp.Dict{
		wamp.MatchExact:    exactRegs,
		wamp.MatchPrefix:   pfxRegs,
		wamp.MatchWildcard: wcRegs,
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{dict},
	}
}

// RegLookup obtains the registration (if any) managing a procedure, according
// to some match policy.
func (d *Dealer) RegLookup(msg *wamp.Invocation) wamp.Message {
	var regID wamp.ID
	if len(msg.Arguments) != 0 {
		if procedure, ok := wamp.AsURI(msg.Arguments[0]); ok {
			var match string
			if len(msg.Arguments) > 1 {
				if opts, ok := wamp.AsDict(msg.Arguments[1]); ok {
					match, _ = wamp.AsString(opts[wamp.OptMatch])
				}
			}
			sync := make(chan wamp.ID)
			d.actionChan <- func() {
				var r wamp.ID
				var reg *registration
				var ok bool
				switch match {
				default:
					reg, ok = d.procRegMap[procedure]
				case wamp.MatchPrefix:
					reg, ok = d.pfxProcRegMap[procedure]
				case wamp.MatchWildcard:
					reg, ok = d.wcProcRegMap[procedure]
				}
				if ok {
					r = reg.id
				}
				sync <- r
			}
			regID = <-sync
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{regID},
	}
}

// RegMatch obtains the registration best matching a given procedure URI.
func (d *Dealer) RegMatch(msg *wamp.Invocation) wamp.Message {
	var regID wamp.ID
	if len(msg.Arguments) != 0 {
		if procedure, ok := wamp.AsURI(msg.Arguments[0]); ok {
			sync := make(chan wamp.ID)
			d.actionChan <- func() {
				var r wamp.ID
				if reg, ok := d.matchProcedure(procedure); ok {
					r = reg.id
				}
				sync <- r
			}
			regID = <-sync
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{regID},
	}
}

// RegGet retrieves information on a particular registration.
func (d *Dealer) RegGet(msg *wamp.Invocation) wamp.Message {
	var dict wamp.Dict
	if len(msg.Arguments) != 0 {
		if regID, ok := wamp.AsID(msg.Arguments[0]); ok {
			sync := make(chan struct{})
			d.actionChan <- func() {
				if reg, ok := d.registrations[regID]; ok {
					dict = wamp.Dict{
						"id":           regID,
						"created":      reg.created,
						"uri":          reg.procedure,
						wamp.OptMatch:  reg.match,
						wamp.OptInvoke: reg.policy,
					}
				}
				close(sync)
			}
			<-sync
		}
	}
	if dict == nil {
		return &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: wamp.Dict{},
			Error:   wamp.ErrNoSuchRegistration,
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{dict},
	}
}

// RegListCallees retrieves a list of session IDs for sessions currently
// attached to the registration.
func (d *Dealer) RegListCallees(msg *wamp.Invocation) wamp.Message {
	var calleeIDs []wamp.ID
	if len(msg.Arguments) != 0 {
		if regID, ok := wamp.AsID(msg.Arguments[0]); ok {
			sync := make(chan struct{})
			d.actionChan <- func() {
				if reg, ok := d.registrations[regID]; ok {
					calleeIDs = make([]wamp.ID, len(reg.callees))
					for i := range reg.callees {
						calleeIDs[i] = reg.callees[i].ID
					}
				}
				close(sync)
			}
			<-sync
		}
	}
	if calleeIDs == nil {
		return &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: wamp.Dict{},
			Error:   wamp.ErrNoSuchRegistration,
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{calleeIDs},
	}
}

// RegCountCallees obtains the number of sessions currently attached to the
// registration.
func (d *Dealer) RegCountCallees(msg *wamp.Invocation) wamp.Message {
	var count int
	var ok bool
	if len(msg.Arguments) != 0 {
		var regID wamp.ID
		if regID, ok = wamp.AsID(msg.Arguments[0]); ok {
			sync := make(chan struct{})
			d.actionChan <- func() {
				if reg, found := d.registrations[regID]; found {
					count = len(reg.callees)
				} else {
					ok = false
				}
				close(sync)
			}
			<-sync
		}
	}
	if !ok {
		return &wamp.Error{
			Type:    msg.MessageType(),
			Request: msg.Request,
			Details: wamp.Dict{},
			Error:   wamp.ErrNoSuchRegistration,
		}
	}
	return &wamp.Yield{
		Request:   msg.Request,
		Arguments: wamp.List{count},
	}
}

func (d *Dealer) trySend(sess *session, msg wamp.Message) bool {
	if err := sess.TrySend(msg); err != nil {
		d.log.Println("!!! dealer dropped", msg.MessageType(), "message:", err)
		return false
	}
	return true
}

// discloseCaller adds caller identity information to INVOCATION.Details.
func discloseCaller(caller *session, details wamp.Dict) {
	details[roleCaller] = caller.ID
	// These values are not required by the specification, but are here for
	// compatibility with Crossbar.
	caller.rLock()
	for _, f := range []string{"authid", "authrole"} {
		if val, ok := caller.Details[f]; ok {
			details[fmt.Sprintf("%s_%s", roleCaller, f)] = val
		}
	}
	caller.rUnlock()
}
