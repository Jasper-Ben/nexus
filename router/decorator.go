package router

import (
	"sort"

	"github.com/gammazero/nexus/wamp"
)

type decoratorMap struct {
	prefixMatch   map[wamp.URI][]*Decorator
	wildcardMatch map[wamp.URI][]*Decorator
	exactMatch    map[wamp.URI][]*Decorator
}

func (dm *decoratorMap) matchDecorators(procedure wamp.URI) []*Decorator {
	decorators := []*Decorator{}

	exactList := dm.exactMatch[procedure]
	decorators = append(decorators, exactList...)
	for pfxURI, pfxDecList := range dm.prefixMatch {
		if !procedure.PrefixMatch(pfxURI) {
			continue
		}
		decorators = append(decorators, pfxDecList...)
	}
	for wcURI, wcDecList := range dm.wildcardMatch {
		if !procedure.WildcardMatch(wcURI) {
			continue
		}
		decorators = append(decorators, wcDecList...)
	}
	sort.Slice(decorators, func(i, j int) bool {
		return decorators[i].order < decorators[j].order
	})
	return decorators
}

func newDecoratorMap() *decoratorMap {
	return &decoratorMap{
		prefixMatch:   make(map[wamp.URI][]*Decorator),
		wildcardMatch: make(map[wamp.URI][]*Decorator),
		exactMatch:    make(map[wamp.URI][]*Decorator),
	}
}

type Decorator struct {
	handlerURI wamp.URI
	order      int64
	callType   wamp.DecoratorCallType
}

func (r *realm) NewDecorator(
	decoratorType wamp.DecoratorType,
	matchURI wamp.URI,
	matchType string,
	handlerURI wamp.URI,
	order int64,
	callType wamp.DecoratorCallType,
) (*Decorator, wamp.URI) {

	_, hasRegistration := r.dealer.matchProcedure(handlerURI)

	if !hasRegistration {
		return nil, wamp.ErrNoSuchProcedure
	}

	createdDecorator := Decorator{
		handlerURI,
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

	matchURI, isOk := wamp.AsURI(msg.Arguments[1])
	if !isOk {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}
	matchType, isOk := wamp.AsString(msg.Arguments[2])
	if !isOk {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}
	handlerURI, isOk := wamp.AsURI(msg.Arguments[3])
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

	createdDecorator, errURI := r.NewDecorator(decoratorType, matchURI, matchType, handlerURI, order, callType)

	// TODO: Think about empty string as empty wamp uri.
	if errURI != "" {
		return makeError(msg.Request, errURI)
	}

	decoratorID := wamp.GlobalID()
	r.decorators[decoratorID] = createdDecorator

	r.log.Printf("Created Decorator with ID %v", decoratorID)

	return &wamp.Yield{Request: msg.Request, Arguments: wamp.List{decoratorID}}

}

func (r *realm) RemoveDecoratorHandler(msg *wamp.Invocation) wamp.Message {

	decoratorID, isOk := wamp.AsID(msg.Arguments[0])
	if !isOk {
		return makeError(msg.Request, wamp.ErrInvalidArgument)
	}

	r.log.Printf("Removing Decorator with ID %v", decoratorID)
	delete(r.decorators, decoratorID)

	return &wamp.Yield{Request: msg.Request, Arguments: wamp.List{decoratorID}}
}
