// Copyright 2023 NJWS Inc.

package uilib

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/embedded/graph/common"
	"github.com/foliagecp/sdk/statefun"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
)

var (
	extractFuncRegex = regexp.MustCompile(`(.*?)\(.*\)`)
	extractArgsRegex = regexp.MustCompile(`\(\s*([^)]+?)\s*\)`)
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

@function:<function name>:[[arg1 value],[arg2 value],...[argN value]]
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

	reply := easyjson.NewJSONObject()
	status := "ok"
	var reasonErr error

_Loop:
	for _, key := range payload.ObjectKeys() {
		bodyValue, bodyExists := payload.GetByPath(key).AsString()
		if !bodyExists || bodyValue[0] != '@' {
			reply.SetByPath(key, easyjson.NewJSON(bodyValue))
			continue
		}

		tokens := strings.Split(bodyValue, ":")
		decorator := strings.TrimSpace(tokens[0])

		switch decorator {
		case "@property":
			{
				if len(tokens) == 1 {
					status = "failed"
					reasonErr = fmt.Errorf("@property: incorrect declaration for key=%s", bodyValue)
					break _Loop
				}

				path := strings.TrimSpace(tokens[1])

				if !objectContext.PathExists(path) {
					status = "failed"
					reasonErr = fmt.Errorf("@property: object with uuid=%s does not contain property by json path=%s (at controller key=%s)", objectID, path, key)
					break _Loop
				}

				reply.SetByPath(key, objectContext.GetByPath(path))
			}
		case "@function":
			{
				functionWithArgs := strings.TrimSpace(bodyValue[len("@function:"):])

				matchFunc := extractFuncRegex.FindStringSubmatch(functionWithArgs)
				matchArgs := extractArgsRegex.FindStringSubmatch(functionWithArgs)

				if len(matchFunc) != 2 {
					status = "failed"
					reasonErr = fmt.Errorf("@function: invalid function format: %s", functionWithArgs)
					break _Loop
				}

				funcName := matchFunc[1]
				funcArgs := make([]string, 0)

				if len(matchArgs) == 2 {
					funcArgs = strings.Split(matchArgs[1], ",")
				}

				var funcResult []string

				switch funcName {
				case "getChildrenUUIDSByLinkType":
					{
						switch len(funcArgs) {
						case 0:
							funcResult = getChildrenUUIDSByLinkType(ctxProcessor, objectID, "")
						case 1:
							funcResult = getChildrenUUIDSByLinkType(ctxProcessor, objectID, funcArgs[0])
						default:
							status = "failed"
							reasonErr = fmt.Errorf("@function: invalid arguments count %d for funtion %s", len(funcArgs), funcName)
							break _Loop
						}
					}
				case "getOutLinkTypes":
					switch len(funcArgs) {
					case 0:
						funcResult = getOutLinkTypes(ctxProcessor, objectID)
					default:
						status = "failed"
						reasonErr = fmt.Errorf("@function: invalid arguments count %d for funtion %s", len(funcArgs), funcName)
						break _Loop
					}
				default:
					status = "failed"
					reasonErr = fmt.Errorf("@function: unknown function: %s", funcName)
					break _Loop
				}

				reply.SetByPath(key, easyjson.JSONFromArray(funcResult))
			}
		default:
			slog.Warn("controller_value_decorator: unknown decorator", "decorator", decorator)
			reply.SetByPath(key, easyjson.NewJSONNull())
		}
	}

	result := easyjson.NewJSONObject()

	if status != "ok" {
		slog.Warn(reasonErr.Error())

		result.SetByPath("result", easyjson.NewJSON(reasonErr.Error()))
	} else {
		result.SetByPath("result", reply)
	}

	result.SetByPath("status", easyjson.NewJSON(status))
	if err := ctxProcessor.Signal(sfplugins.JetstreamGlobalSignal, ctxProcessor.Caller.Typename, ctxProcessor.Caller.ID, &result, nil); err != nil {
		slog.Warn(err.Error())
	}
}
