

package uilib

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/embedded/graph/common"
	"github.com/foliagecp/sdk/statefun"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
)

const (
	_PROPERTY = "@property"
	_FUNCTION = "@function"
)

/*
	payload: {
		body:{},
	}

	controller_name: {
		body: {...},
		subscribers: as links
		construct: {}
	},
*/
func (h *statefunHandler) setupController(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	const op = "setupController"

	self := ctxProcessor.Self
	caller := ctxProcessor.Caller
	payload := ctxProcessor.Payload

	queryID := common.GetQueryID(ctxProcessor)

	rev, err := statefun.KeyMutexLock(h.runtime, self.ID, false, op)
	if err != nil {
		return
	}

	defer func() {
		if err := statefun.KeyMutexUnlock(h.runtime, self.ID, rev, op); err != nil {
			slog.Warn("Key mutex unlock", "caller", op, "error", err)
		}
	}()

	object := ctxProcessor.GetObjectContext()

	now := time.Now()

	if object.PathExists(caller.ID) {
		last, _ := object.GetByPath(caller.ID).AsNumeric()
		if int64(last)+int64(time.Second*2) > now.UnixNano() {
			return
		}
	}

	object.SetByPath(caller.ID, easyjson.NewJSON(time.Now().UnixNano()))

	split := strings.Split(self.ID, "_")

	controllerName := split[0]
	controllerUUID := split[len(split)-1]

	if payload.PathExists("result") {
		return
	}

	bodyIsEmpty := !object.GetByPath("body").IsNonEmptyObject()

	if bodyIsEmpty {
		controllerBody := easyjson.NewJSONObject()
		controllerBody.SetByPath("body", payload.GetByPath("body"))
		controllerBody.SetByPath("construct", easyjson.NewJSONObject())

		ctxProcessor.SetObjectContext(&controllerBody)

		if err := createLink(ctxProcessor, self.ID, caller.ID, "subscriber", easyjson.NewJSONObject().GetPtr(), caller.ID); err != nil {
			slog.Error("Cannot create link", "error", err)
			return
		}

		updatePayload := easyjson.NewJSONObject()
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, triggerSubscriberUpdateFunction, self.ID, &updatePayload, nil); err != nil {
			slog.Warn(err.Error())
		}

		//subscribe on objects for update
		triggerID := "trigger_" + controllerUUID
		triggerCreatePayload := easyjson.NewJSONObject()
		triggerCreatePayload.SetByPath("subscriber", easyjson.NewJSON(self.ID))
		triggerCreatePayload.SetByPath("destination", easyjson.NewJSON(controllerUUID))
		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, triggerCreateFunction, triggerID, &triggerCreatePayload, nil); err != nil {
			slog.Warn(err.Error())
		}
	} else {
		subscribers := getChildrenUUIDSByLinkType(ctxProcessor, self.ID, "subscriber")

		for _, v := range subscribers {
			if v == caller.ID {
				return
			}
		}

		if err := createLink(ctxProcessor, self.ID, caller.ID, "subscriber", easyjson.NewJSONObject().GetPtr(), caller.ID); err != nil {
			slog.Error("Cannot create link", "error", err)
			return
		}

		if construct := object.GetByPath("construct"); construct.IsNonEmptyObject() {
			path := fmt.Sprintf("payload.controllers.%s.%s", controllerName, controllerUUID)

			reply := easyjson.NewJSONObject()
			reply.SetByPath(path, construct)

			if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, clientEgressFunction, caller.ID, &reply, nil); err != nil {
				slog.Warn(err.Error())
			}
		}

		//subscribe on objects for update
		triggerID := "trigger_" + controllerUUID
		triggerCreatePayload := easyjson.NewJSONObject()
		triggerCreatePayload.SetByPath("subscriber", easyjson.NewJSON(self.ID))
		triggerCreatePayload.SetByPath("destination", easyjson.NewJSON(controllerUUID))

		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, triggerCreateFunction, triggerID, &triggerCreatePayload, nil); err != nil {
			slog.Warn(err.Error())
		}
	}

	// if strings.Contains(controllerUUID, "leds") {
	// 	ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal,ledAutoSwitchFunction, controllerUUID, easyjson.NewJSONObject().GetPtr(), nil)
	// }

	result := easyjson.NewJSONObject()
	result.SetByPath("status", easyjson.NewJSON("ok"))
	result.SetByPath("result", easyjson.NewJSON(""))

	common.ReplyQueryID(queryID, &result, ctxProcessor)
}

func (h *statefunHandler) unsubController(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	const op = "unsubController"

	caller := ctxProcessor.Caller
	self := ctxProcessor.Self
	queryID := common.GetQueryID(ctxProcessor)

	rev, err := statefun.KeyMutexLock(h.runtime, self.ID, false, op)
	if err != nil {
		return
	}

	defer func() {
		if err := statefun.KeyMutexUnlock(h.runtime, self.ID, rev, op); err != nil {
			slog.Warn("Key mutex unlock", "caller", op, "error", err)
		}
	}()

	link := easyjson.NewJSONObject()
	link.SetByPath("descendant_uuid", easyjson.NewJSON(caller.ID))
	link.SetByPath("link_type", easyjson.NewJSON("subscriber"))

	if _, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, "functions.graph.ll.api.link.delete", self.ID, &link, nil); err != nil {
		slog.Warn("Cannot delete link", "error", err)
		return
	}

	subs := getChildrenUUIDSByLinkType(ctxProcessor, self.ID, "subscriber")
	if len(subs) == 0 {
		deleteObjectPayload := easyjson.NewJSONObject()
		_, err := ctxProcessor.Request(sfplugins.GolangLocalRequest, "functions.graph.ll.api.object.delete", self.ID, &deleteObjectPayload, nil)
		if err != nil {
			slog.Warn("Cannot delete object", "error", err)
		}
	}

	result := easyjson.NewJSONObject()
	result.SetByPath("status", easyjson.NewJSON("ok"))
	result.SetByPath("result", easyjson.NewJSON(""))

	common.ReplyQueryID(queryID, &result, ctxProcessor)
}

/*
@property:<json path>

@function:<function name>:[[arg1 value],[arg2 value],...[argN value]] - ideal

@function:getChildren(linkType) - now
*/
func (h *statefunHandler) createControllerConstruct(_ sfplugins.StatefunExecutor, ctxProcessor *sfplugins.StatefunContextProcessor) {
	objectID := ctxProcessor.Self.ID
	objectContext := ctxProcessor.GetObjectContext()
	payload := ctxProcessor.Payload

	if !objectContext.IsNonEmptyObject() {
		result := easyjson.NewJSONObject()
		result.SetByPath("result", easyjson.NewJSON(fmt.Errorf("%v is empty objects", objectID)))
		result.SetByPath("status", easyjson.NewJSON("failed"))

		if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, ctxProcessor.Caller.Typename, ctxProcessor.Caller.ID, &result, nil); err != nil {
			slog.Warn(err.Error())
		}

		return
	}

	decorators := parseDecorators(objectID, payload)
	decoratorsReply := easyjson.NewJSONObject()

	for key, cd := range decorators {
		result := cd.Invoke(contextProcessor)
		decoratorsReply.SetByPath(key, result)
	}

	result := easyjson.NewJSONObject()
	result.SetByPath("result", decoratorsReply)
	contextProcessor.Call(contextProcessor.Caller.Typename, contextProcessor.Caller.ID, &result, nil)
}

type controllerDecorator interface {
	Invoke(ctx *sfplugins.StatefunContextProcessor) easyjson.JSON
}

type controllerProperty struct {
	id   string
	path string
}

func (c *controllerProperty) Invoke(ctx *sfplugins.StatefunContextProcessor) easyjson.JSON {
	return ctx.GetObjectContext().GetByPath(c.path)
}

type controllerFunction struct {
	id       string
	function string
	args     []string
}

func (c *controllerFunction) Invoke(ctx *sfplugins.StatefunContextProcessor) easyjson.JSON {
	switch c.function {
	case "getChildrenUUIDSByLinkType":
		lt := ""
		if len(c.args) > 0 {
			lt = c.args[0]
		}

		children := getChildrenUUIDSByLinkType(ctx, c.id, lt)
		return easyjson.NewJSON(children)
	case "getOutLinkTypes":
		outLinks := getOutLinkTypes(ctx, c.id)
		return easyjson.NewJSON(outLinks)
	}

	return easyjson.NewJSONNull()
}

func parseDecorators(objectID string, payload *easyjson.JSON) map[string]controllerDecorator {
	decorators := make(map[string]controllerDecorator)

	for _, key := range payload.ObjectKeys() {
		body := payload.GetByPath(key).AsStringDefault("")
		tokens := strings.Split(body, ":")
		if len(tokens) < 2 {
			continue
		}

		decorator := tokens[0]
		value := tokens[1]

		switch decorator {
		case _PROPERTY:
			// TODO: add check value
			decorators[key] = &controllerProperty{
				id:   objectID,
				path: value,
			}
		case _FUNCTION:
			{
				f, args, err := extractFunctionAndArgs(value)
				if err != nil {
					slog.Warn(err.Error())
					continue
				}

				decorators[key] = &controllerFunction{
					id:       objectID,
					function: f,
					args:     args,
				}
			}
		default:
			slog.Warn("parse decorator: unknown decorator", "decorator", decorator)
		}
	}

	return decorators
}

func extractFunctionAndArgs(s string) (string, []string, error) {
	split := strings.Split(strings.TrimRight(s, ")"), "(")
	if len(split) != 2 {
		return "", nil, fmt.Errorf("@function: invalid function format: %s", s)
	}

	funcName := split[0]
	funcArgs := strings.Split(strings.TrimSpace(split[1]), ",")

	return funcName, funcArgs, nil
}
