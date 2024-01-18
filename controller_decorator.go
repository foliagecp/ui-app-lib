package uilib

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/foliagecp/easyjson"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
)

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
		return easyjson.JSONFromArray(children)
	case "getInOutLinkTypes":
		out := getInOutLinkTypes(ctx, c.id)
		return easyjson.JSONFromArray(out)
	case "getOutLinkTypes":
		out := getOutLinkTypes(ctx, c.id)
		return easyjson.JSONFromArray(out)
	case "getLinksByType":
		lt := c.args[0]
		out := getLinksByType(ctx, c.id, lt)
		return easyjson.JSONFromArray(out)
	}

	return easyjson.NewJSONObject()
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
