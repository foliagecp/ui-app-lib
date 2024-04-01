package decorators

import (
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
	current := ctx.Self.ID
	cmdb := common.MustCMDBClient(ctx.Request)

	radius := int(ctx.Payload.GetByPath("radius").AsNumericDefault(1))

	maxRadius := radius
	if radius < 0 {
		maxRadius = 10 // make const
	}

	links := make([]link, 0)
	nodes := make([]routeNode, 0)

	stack := []routeNode{
		{ID: current, Depth: 0},
	}

	visited := make(map[string]struct{})
	visited[current] = struct{}{}

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

		nodes = append(nodes, routeNode{
			ID:    current.ID,
			Name:  alias,
			Depth: current.Depth,
			Pos:   pos,
		})

		if current.Depth >= maxRadius {
			continue
		}

		outTypes, _ := typeBody.GetByPath("to_types").AsArrayString()

		for _, v := range inOutTypes(ctx, current.ID) {
			if _, ok := visited[v]; ok {
				continue
			}

			visited[v] = struct{}{}

			tbody, err := cmdb.TypeRead(v)
			if err != nil {
				continue
			}

			viewNavigation := tbody.GetByPath("body.view_navigation").AsBoolDefault(false)
			if !viewNavigation {
				continue
			}

			isOut := false
			for _, outType := range outTypes {
				if outType == v {
					isOut = true
					break
				}
			}

			var l link

			if isOut {
				l = link{
					Source: current.ID,
					Target: v,
				}
			} else {
				l = link{
					Source: v,
					Target: current.ID,
				}
			}

			links = append(links, l)
			stack = append(stack, routeNode{
				ID:    v,
				Depth: current.Depth + 1,
			})
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
