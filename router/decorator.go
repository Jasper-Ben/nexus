package router

import "github.com/gammazero/nexus/wamp"

type Decorator struct {
	decoratorType wamp.DecoratorType

	matchUri wamp.URI
	// TODO: Create wamp.MatchType
	matchType  string
	handlerUri wamp.URI
	order      int64
	callType   wamp.DecoratorCallType
}

func (r *realm) NewDecorator(
	decoratorType wamp.DecoratorType,
	matchUri wamp.URI,
	matchType string,
	handlerUri wamp.URI,
	order int64,
	callType wamp.DecoratorCallType,
) (*Decorator, wamp.URI) {

	_, hasRegistration := r.dealer.matchProcedure(handlerUri)

	if !hasRegistration {
		return nil, wamp.ErrNoSuchProcedure
	}

	createdDecorator := Decorator{
		decoratorType,
		matchUri,
		matchType,
		handlerUri,
		order,
		callType,
	}

	return &createdDecorator, ""

}

func (r *realm) AddDecoratorHandler(msg *wamp.Invocation) wamp.Message {

	r.log.Print("AddDecoratorHandler called")

	decoratorString, isOk := wamp.AsString(msg.Arguments[0])
	if !isOk {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}

	var decoratorType wamp.DecoratorType

	switch wamp.DecoratorType(decoratorString) {
	case "preprocess":
		decoratorType = wamp.DecoratorTypePreprocess
	case "precall":
		decoratorType = wamp.DecoratorTypePrecall
	case "postcall":
		decoratorType = wamp.DecoratorTypePostcall
	case "publish":
		decoratorType = wamp.DecoratorTypePublish
	case "event":
		decoratorType = wamp.DecoratorTypeEvent
	default:
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}

	matchUri, isOk := wamp.AsURI(msg.Arguments[1])
	if !isOk {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}
	matchType, isOk := wamp.AsString(msg.Arguments[2])
	if !isOk {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}
	handlerUri, isOk := wamp.AsURI(msg.Arguments[3])
	if !isOk {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}
	order, isOk := wamp.AsInt64(msg.Arguments[4])
	if !isOk {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}

	callTypeString, isOk := wamp.AsString(msg.Arguments[5])
	if !isOk {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}

	var callType wamp.DecoratorCallType

	switch wamp.DecoratorCallType(callTypeString) {
	case "sync":
		callType = wamp.DecoratorCallTypeSync
	case "async":
		callType = wamp.DecoratorCallTypeAsync
	default:
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}

	createdDecorator, errUri := r.NewDecorator(decoratorType, matchUri, matchType, handlerUri, order, callType)

	// TODO: Think about empty string as empty wamp uri.
	if errUri != "" {
		return makeError(msg.Request, errUri)
	}

	decoratorId := wamp.GlobalID()
	r.decorators[decoratorId] = createdDecorator

	r.log.Printf("Created Decorator with Id %v", decoratorId)

	return &wamp.Yield{Request: msg.Request, Arguments: wamp.List{decoratorId}}

}

func (r *realm) RemoveDecoratorHandler(msg *wamp.Invocation) wamp.Message {

	decoratorId, isOk := wamp.AsID(msg.Arguments[0])
	if !isOk {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}

	r.log.Printf("Removing Decorator with Id %v", decoratorId)
	delete(r.decorators, decoratorId)

	return &wamp.Yield{Request: msg.Request, Arguments: wamp.List{decoratorId}}
}
