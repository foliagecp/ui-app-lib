package decorators

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/foliagecp/sdk/embedded/graph/crud"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/ui-app-lib/internal/common"
)

type typesRouteView struct {
	Nodes []routeNode `json:"nodes"`
	Links []link      `json:"links"`
}

type routeNode struct {
	ID      string        `json:"id"`
	Name    string        `json:"name,omitempty"`
	Pos     any           `json:"pos"`
	Depth   int           `json:"-"`
	Objects []routeObject `json:"objects,omitempty"`
}

type routeObject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

/*
	Request: {
		"radius"
	}

	Response: {
		"status"
		"message"
		"data": {
			"nodes": []{
				"id"
				"name" // alias
				"pos"
				"objects"
			},
			"links": []{
				"source"
				"target"
				"type"
				"tags"
			}
		}
	}
*/
func typesNavigation(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	currentObjectID := ctx.Self.ID
	cmdb := common.MustCMDBClient(ctx.Request)

	objectBody, err := cmdb.ObjectRead(currentObjectID)
	if err != nil {
		errResponse(ctx, "failed to read object")
		return
	}

	objectType, ok := objectBody.GetByPath("type").AsString()
	if !ok {
		errResponse(ctx, "missing object type")
		return
	}

	objectsOutLinks := make(map[string]string)

	for _, key := range ctx.Domain.Cache().GetKeysByPattern(common.OutLinkType(ctx.Self.ID, ">")) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		linkname, err := ctx.Domain.Cache().GetValue(key)
		if err != nil {
			continue
		}

		lastkey := split[len(split)-1]
		objectsOutLinks[lastkey] = string(linkname)
	}

	radius := int(ctx.Payload.GetByPath("radius").AsNumericDefault(1))

	maxRadius := radius
	if radius < 0 {
		maxRadius = 10 // make const
	}

	links := make([]link, 0)
	nodes := make([]routeNode, 0)

	stack := []routeNode{
		{ID: objectType, Depth: 0},
	}

	visited := make(map[string]struct{})
	visited[objectType] = struct{}{}

	for len(stack) > 0 {
		current := stack[0]
		stack = stack[1:]

		typeBody, err := cmdb.TypeRead(current.ID)
		if err != nil {
			continue
		}

		//"body": {"alias":"contour", "pos":[0, 2], "view_navigation": true}
		viewNavigation := typeBody.GetByPath("body.view_navigation").AsBoolDefault(false)
		if !viewNavigation {
			continue
		}

		alias := typeBody.GetByPath("body.alias").AsStringDefault("")
		pos, _ := typeBody.GetByPath("body.pos").AsArray()

		node := routeNode{
			ID:    current.ID,
			Name:  alias,
			Depth: current.Depth,
			Pos:   pos,
		}

		if current.Depth == 1 && current.ID != objectType {

			if ok := typeBody.PathExists("object_ids"); ok {
				var typeObjects []string
				if err := json.Unmarshal(typeBody.GetByPath("object_ids").ToBytes(), &typeObjects); err != nil {
					slog.Warn(err.Error())
				}

				for _, v := range typeObjects {
					linkname, ok := objectsOutLinks[v]
					if !ok {
						continue
					}

					node.Objects = append(node.Objects, routeObject{
						ID:   v,
						Name: linkname,
					})
				}
			}

		}

		nodes = append(nodes, node)

		if current.Depth >= maxRadius {
			continue
		}

		outTypes, _ := typeBody.GetByPath("to_types").AsArrayString()

		for _, ioType := range inOutTypes(ctx, current.ID) {
			tbody, err := cmdb.TypeRead(ioType)
			if err != nil {
				continue
			}

			viewNavigation := tbody.GetByPath("body.view_navigation").AsBoolDefault(false)
			if !viewNavigation {
				continue
			}

			isOut := false
			for _, outType := range outTypes {
				if outType == ioType {
					isOut = true
					break
				}
			}

			var l link

			if isOut {
				l = link{
					Source: current.ID,
					Target: ioType,
				}
			} else {
				l = link{
					Source: ioType,
					Target: current.ID,
				}
			}

			if _, ok := visited[l.Source+l.Target]; !ok {
				links = append(links, l)
			}

			visited[l.Source+l.Target] = struct{}{}

			if _, ok := visited[ioType]; !ok {
				stack = append(stack, routeNode{
					ID:    ioType,
					Depth: current.Depth + 1,
				})
			}

			visited[ioType] = struct{}{}
		}
	}

	nav := &typesRouteView{
		Links: links,
		Nodes: nodes,
	}

	okResponse(ctx, nav)
}

func inOutTypes(ctx *sf.StatefunContextProcessor, id string) []string {
	c := common.MustCMDBClient(ctx.Request)
	list := make([]string, 0)

	inPattern := common.InLinkKeyPattern(id, ">")
	for _, key := range ctx.Domain.Cache().GetKeysByPattern(inPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		objectID := split[len(split)-2]

		link, err := c.TypesLinkRead(objectID, id)
		if err != nil {
			slog.Error(err.Error())
			continue
		}

		if !link.GetByPath("body").IsNonEmptyObject() {
			continue
		}

		linkType, _ := link.GetByPath("type").AsString()

		if linkType != crud.TO_TYPELINK {
			continue
		}

		list = append(list, objectID)
	}

	outPattern := common.OutLinkType(id, crud.TO_TYPELINK, ">")

	for _, key := range ctx.Domain.Cache().GetKeysByPattern(outPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		list = append(list, split[len(split)-1])
	}

	return list
}
