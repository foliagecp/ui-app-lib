package adapter

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/foliagecp/easyjson"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
)

// decorators
const (
	_PROPERTY = "@property"
	_FUNCTION = "@function"
)

type controllerDecorator interface {
	Decorate(ctx *sf.StatefunContextProcessor) easyjson.JSON
}

type controllerProperty struct {
	id   string
	path string
}

func (c *controllerProperty) Decorate(ctx *sf.StatefunContextProcessor) easyjson.JSON {
	return ctx.GetObjectContext().GetByPath(c.path)
}

type controllerFunction struct {
	id       string
	function string
	args     []string
}

func (c *controllerFunction) Decorate(ctx *sf.StatefunContextProcessor) easyjson.JSON {
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
		if len(c.args) != 1 {
			return easyjson.NewJSON("invalid arguments")
		}

		lt := c.args[0]
		out := getLinksByType(ctx, c.id, lt)
		return easyjson.NewJSON(out)
	case "typesNavigation":
		if len(c.args) != 1 {
			return easyjson.NewJSON("invalid arguments")
		}

		radius, _ := strconv.Atoi(c.args[0])
		return typesNavigation(ctx, c.id, radius)
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

func getChildrenUUIDSByLinkType(ctx *sf.StatefunContextProcessor, id, filterLinkType string) []string {
	payload := easyjson.NewJSONObject()
	payload.SetByPath("link_type", easyjson.NewJSON(filterLinkType))

	fmt.Println("!!!!!!!!!!!! getChildrenUUIDSByLinkType: make request")

	result, err := ctx.Request(sf.AutoRequestSelect, inStatefun.CHILDREN_LINK_TYPE_DECORATOR, id, &payload, nil)
	if err != nil {
		slog.Error(err.Error())
		return []string{}
	}

	fmt.Println("!!!!!!!!!!!! getChildrenUUIDSByLinkType: result", result.ToString())

	if result.GetByPath("status").AsStringDefault("failed") == "failed" {
		slog.Error(result.GetByPath("message").AsString())
		return []string{}
	}

	var list []string
	if err := json.Unmarshal(result.GetByPath("data").ToBytes(), &list); err != nil {
		return []string{}
	}

	return list
}

func getInOutLinkTypes(ctx *sf.StatefunContextProcessor, id string) []string {
	payload := easyjson.NewJSONObject()

	result, err := ctx.Request(sf.AutoRequestSelect, inStatefun.IO_LINK_TYPES_DECORATOR, id, &payload, nil)
	if err != nil {
		return []string{}
	}

	if result.GetByPath("status").AsStringDefault("failed") == "failed" {
		return []string{}
	}

	in, _ := result.GetByPath("data.in").AsArrayString()
	out, _ := result.GetByPath("data.out").AsArrayString()

	return append(in, out...)
}

func getOutLinkTypes(ctx *sf.StatefunContextProcessor, id string) []string {
	payload := easyjson.NewJSONObject()

	result, err := ctx.Request(sf.AutoRequestSelect, inStatefun.IO_LINK_TYPES_DECORATOR, id, &payload, nil)
	if err != nil {
		return []string{}
	}

	if result.GetByPath("status").AsStringDefault("failed") == "failed" {
		return []string{}
	}

	out, _ := result.GetByPath("data.out").AsArrayString()

	return out
}

type Link struct {
	Source string   `json:"source"`
	Target string   `json:"target"`
	Type   string   `json:"type,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

func getLinksByType(ctx *sf.StatefunContextProcessor, id, filterLinkType string) []Link {
	payload := easyjson.NewJSONObject()
	payload.SetByPath("link_type", easyjson.NewJSON(filterLinkType))

	result, err := ctx.Request(sf.AutoRequestSelect, inStatefun.LINKS_TYPE_DECORATOR, id, &payload, nil)
	if err != nil {
		return []Link{}
	}

	if result.GetByPath("status").AsStringDefault("failed") == "failed" {
		return []Link{}
	}

	var links []Link

	if err := json.Unmarshal(result.GetByPath("data").ToBytes(), &links); err != nil {
		return []Link{}
	}

	return links
}

func typesNavigation(ctx *sf.StatefunContextProcessor, id string, radius int) easyjson.JSON {
	payload := easyjson.NewJSONObject()
	payload.SetByPath("radius", easyjson.NewJSON(radius))

	result, err := ctx.Request(sf.AutoRequestSelect, inStatefun.TYPES_NAVIGATION_DECORATOR, id, &payload, nil)
	if err != nil {
		return easyjson.JSON{}
	}

	if result.GetByPath("status").AsStringDefault("failed") == "failed" {
		return easyjson.JSON{}
	}

	return result.GetByPath("data")
}
