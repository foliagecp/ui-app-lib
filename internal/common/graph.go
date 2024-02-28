package common

import (
	"sort"
	"strings"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
)

type Link struct {
	Source string   `json:"source"`
	Target string   `json:"target"`
	Type   string   `json:"type,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

func GetLinksByType(ctx *sf.StatefunContextProcessor, uuid, filterLinkType string) []Link {
	result := make([]Link, 0)

	outPattern := OutLinkType(uuid, filterLinkType, ">")
	for _, key := range ctx.Domain.Cache().GetKeysByPattern(outPattern) {
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

	for _, key := range ctx.Domain.Cache().GetKeysByPattern(inPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		objectID := split[len(split)-2]

		linkType, err := FindObjectType(NewStatefunContext(ctx), objectID)
		if err != nil {
			continue
		}

		linkType = ctx.Domain.GetObjectIDWithoutDomain(linkType)

		if linkType != filterLinkType {
			continue
		}

		result = append(result, Link{
			Source: objectID,
			Target: uuid,
			Type:   filterLinkType,
		})
	}

	return result
}

func GetOutLinkTypes(ctx *sf.StatefunContextProcessor, uuid string) []string {
	outPattern := OutLinkType(uuid, ">")

	result := make([]string, 0)
	visited := make(map[string]struct{})
	s := ctx.Domain.Cache().GetKeysByPattern(outPattern)

	for _, key := range s {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		linkType := split[len(split)-2]
		if isInternalType(linkType) {
			continue
		}

		if _, ok := visited[linkType]; !ok {
			visited[linkType] = struct{}{}
			result = append(result, linkType)
		}
	}

	return result
}

func GetInLinkTypes(ctx *sf.StatefunContextProcessor, uuid string) []string {
	result := make([]string, 0)
	visited := make(map[string]struct{})

	inPattern := InLinkKeyPattern(uuid, ">")

	for _, key := range ctx.Domain.Cache().GetKeysByPattern(inPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		objectID := split[len(split)-2]

		linkType, err := FindObjectType(NewStatefunContext(ctx), objectID)
		if err != nil {
			continue
		}

		linkType = ctx.Domain.GetObjectIDWithoutDomain(linkType)

		if isInternalType(linkType) {
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
	return append(GetOutLinkTypes(ctx, uuid), GetInLinkTypes(ctx, uuid)...)
}

func GetChildrenUUIDSByLinkType(ctx *sf.StatefunContextProcessor, uuid, filterLinkType string) []string {
	result := make([]string, 0)

	if filterLinkType == "" {
		return result
	}

	pattern := OutLinkType(uuid, filterLinkType, ">")
	for _, key := range ctx.Domain.Cache().GetKeysByPattern(pattern) {
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

func GetParentsTypes(ctx *sf.StatefunContextProcessor, typeID string) []string {
	result := make([]string, 0)

	pattern := InLinkKeyPattern(typeID, ">")
	for _, key := range ctx.Domain.Cache().GetKeysByPattern(pattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		objectID := split[len(split)-2]

		if _, err := FindObjectType(NewStatefunContext(ctx), objectID); err == nil {
			continue
		}

		result = append(result, objectID)
	}

	sort.Strings(result)

	return result
}

func GetParentsUUIDSByLinkType(ctx *sf.StatefunContextProcessor, uuid, filterLinkType string) []string {
	result := make([]string, 0)

	pattern := InLinkKeyPattern(uuid, ">")

	for _, key := range ctx.Domain.Cache().GetKeysByPattern(pattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		objectID := split[len(split)-2]

		linkType, err := FindObjectType(NewStatefunContext(ctx), objectID)
		if err != nil {
			continue
		}

		linkType = ctx.Domain.GetObjectIDWithoutDomain(linkType)
		if linkType != filterLinkType {
			continue
		}

		result = append(result, objectID)
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
	t, err := FindObjectType(NewStatefunContext(ctx), id)
	if err != nil {
		return easyjson.NewJSONObjectWithKeyValue("status", easyjson.NewJSON("failed"))
	}

	route := traverseTypesGraph(ctx, t, forward, backward)

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

			if current.Depth >= maxForward {
				continue
			}

			for _, v := range GetChildrenUUIDSByLinkType(ctx, current.ID, crud.TYPE_TYPELINK) {
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

	if maxBackward > 0 {
		stack := []routeNode{
			{ID: startID, Name: startID, Depth: 0},
		}

		for len(stack) > 0 {
			current := stack[0]
			stack = stack[1:]

			if -current.Depth >= maxBackward {
				continue
			}

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

	return typesRouteView{
		Type:  startID,
		Links: links,
		Nodes: nodes,
	}
}

func GetTypeObjects(ctx *sf.StatefunContextProcessor, typeID string) []routeObject {
	keys := GetChildrenUUIDSByLinkType(ctx, typeID, crud.OBJECT_TYPELINK)
	objects := make([]routeObject, 0, len(keys))

	for _, object := range keys {
		objects = append(objects, routeObject{
			Name: object,
			ID:   object,
		})
	}

	return objects
}

func isInternalType(t string) bool {
	switch t {
	case crud.OBJECT_TYPELINK,
		crud.TYPE_TYPELINK,
		crud.TYPES_TYPELINK,
		crud.OBJECTS_TYPELINK,
		inStatefun.SESSION_TYPE,
		inStatefun.CONTROLLER_TYPE,
		inStatefun.CONTROLLER_SUBJECT_TYPE,
		inStatefun.SUBSCRIBER_TYPE:
		return true
	}

	return false
}
