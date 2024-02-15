package common

import (
	"sort"
	"strings"

	"github.com/foliagecp/easyjson"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	internalStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
)

type Link struct {
	Source string   `json:"source"`
	Target string   `json:"target"`
	Type   string   `json:"type,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

func GetLinksByType(ctx *sf.StatefunContextProcessor, uuid, filterLinkType string) []Link {
	result := make([]Link, 0)

	outPattern := OutLinkKeyPattern(uuid, ">", filterLinkType)
	for _, key := range ctx.GlobalCache.GetKeysByPattern(outPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		result = append(result, Link{
			Source: uuid,
			Target: split[len(split)-1],
			Type:   filterLinkType,
		})
	}

	inPattern := InLinkKeyPattern(uuid, ">")
	for _, key := range ctx.GlobalCache.GetKeysByPattern(inPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		ltype := split[len(split)-1]

		if ltype != filterLinkType {
			continue
		}

		result = append(result, Link{
			Source: split[len(split)-2],
			Target: uuid,
			Type:   filterLinkType,
		})
	}

	return result
}

func GetOutLinkTypes(ctx *sf.StatefunContextProcessor, uuid string) []string {
	outPattern := OutLinkKeyPattern(uuid, ">")

	result := make([]string, 0)
	visited := make(map[string]struct{})

	for _, key := range ctx.GlobalCache.GetKeysByPattern(outPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		linkType := split[len(split)-2]

		// TODO: use builtin constants
		switch linkType {
		case "__object", "__type", "__types", "__objects", internalStatefun.SESSION_TYPE, internalStatefun.CONTROLLER_TYPE, internalStatefun.CONTROLLER_SUBJECT_TYPE, internalStatefun.SUBSCRIBER_TYPE:
			continue
		}

		if _, ok := visited[linkType]; !ok {
			visited[linkType] = struct{}{}
			result = append(result, linkType)
		}
	}

	return result
}

func GetInOutLinkTypes(ctx *sf.StatefunContextProcessor, uuid string) []string {
	outPattern := OutLinkKeyPattern(uuid, ">")

	result := make([]string, 0)
	visited := make(map[string]struct{})

	for _, key := range ctx.GlobalCache.GetKeysByPattern(outPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		linkType := split[len(split)-2]

		// TODO: use builtin constants
		switch linkType {
		case "__object", "__type", "__types", "__objects", internalStatefun.SESSION_TYPE, internalStatefun.CONTROLLER_TYPE, internalStatefun.CONTROLLER_SUBJECT_TYPE, internalStatefun.SUBSCRIBER_TYPE:
			continue
		}

		if _, ok := visited[linkType]; !ok {
			visited[linkType] = struct{}{}
			result = append(result, linkType)
		}
	}

	inPattern := InLinkKeyPattern(uuid, ">")
	for _, key := range ctx.GlobalCache.GetKeysByPattern(inPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		linkType := split[len(split)-1]

		// TODO: use builtin constants
		switch linkType {
		case "__object", "__type", "__types", "__objects", internalStatefun.SESSION_TYPE, internalStatefun.CONTROLLER_TYPE, internalStatefun.CONTROLLER_SUBJECT_TYPE, internalStatefun.SUBSCRIBER_TYPE:
			continue
		}

		if _, ok := visited[linkType]; !ok {
			visited[linkType] = struct{}{}
			result = append(result, linkType)
		}
	}

	return result
}

func GetChildrenUUIDSByLinkType(ctx *sf.StatefunContextProcessor, uuid, filterLinkType string) []string {
	result := make([]string, 0)

	pattern := OutLinkKeyPattern(uuid, ">")
	if filterLinkType != "" {
		pattern = OutLinkKeyPattern(uuid, ">", filterLinkType)
	}

	for _, key := range ctx.GlobalCache.GetKeysByPattern(pattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		lastkey := split[len(split)-1]
		result = append(result, lastkey)
	}

	sort.Strings(result)

	return result
}

func GetParentsTypes(ctx *sf.StatefunContextProcessor, uuid string) []string {
	result := make([]string, 0)

	for _, v := range GetParentsUUIDSByLinkType(ctx, uuid, "__type") {
		if _, err := ctx.GlobalCache.GetValue(OutLinkKeyPattern("types", v, "__type")); err != nil {
			continue
		}

		result = append(result, v)
	}

	sort.Strings(result)

	return result
}

func GetParentsUUIDSByLinkType(ctx *sf.StatefunContextProcessor, uuid, filterLinkType string) []string {
	result := make([]string, 0)

	pattern := InLinkKeyPattern(uuid, ">")

	for _, key := range ctx.GlobalCache.GetKeysByPattern(pattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		linkType := split[len(split)-1]
		if linkType != filterLinkType {
			continue
		}

		lastkey := split[len(split)-2]
		result = append(result, lastkey)
	}

	sort.Strings(result)

	return result
}

type typesRouteView struct {
	Type  string      `json:"type"`
	Nodes []routeNode `json:"nodes"`
	Links []Link      `json:"links"`
}

type routeNode struct {
	ID      string        `json:"uuid"`
	Name    string        `json:"name"`
	Depth   int           `json:"depth"`
	Objects []routeObject `json:"objects"`
}

type routeObject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TypesNavigation(ctx *sf.StatefunContextProcessor, id string, forward, backward int) easyjson.JSON {
	types := GetChildrenUUIDSByLinkType(ctx, id, "__type")
	if len(types) == 0 {
		return easyjson.NewJSONObjectWithKeyValue("status", easyjson.NewJSON("failed"))
	}

	originType := types[0]
	route := traverseTypesGraph(ctx, originType, forward, backward)

	return easyjson.NewJSON(route)
}

func traverseTypesGraph(ctx *sf.StatefunContextProcessor, startID string, forward, backward int) typesRouteView {
	maxForward := forward
	if forward < 0 {
		maxForward = 10 // make const
	}

	maxBackward := backward
	if backward < 0 {
		maxBackward = 10 // make const
	}

	links := make([]Link, 0)
	nodes := make([]routeNode, 0)

	if maxForward > 0 {
		stack := []routeNode{
			{ID: startID, Name: startID, Depth: 0},
		}

		for len(stack) > 0 {
			current := stack[0]
			stack = stack[1:]

			if current.Depth < maxForward {
				for _, v := range GetChildrenUUIDSByLinkType(ctx, current.ID, "__type") {
					node := routeNode{
						ID:      v,
						Name:    v,
						Depth:   current.Depth + 1,
						Objects: GetTypeObjects(ctx, v),
					}

					link := Link{
						Source: current.ID,
						Target: v,
					}

					links = append(links, link)
					nodes = append(nodes, node)
					stack = append(stack, node)
				}
			}
		}
	}

	if maxBackward > 0 {
		stack := []routeNode{
			{ID: startID, Name: startID, Depth: 0},
		}

		for len(stack) > 0 {
			current := stack[0]
			stack = stack[1:]

			if -current.Depth < maxBackward {
				for _, v := range GetParentsTypes(ctx, current.ID) {
					node := routeNode{
						ID:      v,
						Name:    v,
						Depth:   current.Depth - 1,
						Objects: GetTypeObjects(ctx, v),
					}

					link := Link{
						Source: v,
						Target: current.ID,
					}

					links = append(links, link)
					nodes = append(nodes, node)
					stack = append(stack, node)
				}
			}
		}
	}

	return typesRouteView{
		Type:  startID,
		Links: links,
		Nodes: nodes,
	}
}

func GetTypeObjects(ctx *sf.StatefunContextProcessor, typeID string) []routeObject {
	keys := GetChildrenUUIDSByLinkType(ctx, typeID, "__object")
	objects := make([]routeObject, 0, len(keys))

	for _, object := range keys {
		objects = append(objects, routeObject{
			Name: object,
			ID:   object,
		})
	}

	return objects
}
