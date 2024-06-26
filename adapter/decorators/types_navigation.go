package decorators

import (
	"encoding/json"
	"log/slog"
	"sort"

	"github.com/foliagecp/sdk/embedded/graph/crud"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/ui-app-lib/internal/common"
)

type typesNav struct {
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
	db := common.MustDBClient(ctx.Request)

	data, err := db.Graph.VertexRead(currentObjectID)
	if err != nil {
		errResponse(ctx, "failed to read object")
		return
	}

	objectsOutLinks := map[string]string{}

	objectType := ""
	for i := 0; i < data.GetByPath("links.out.names").ArraySize(); i++ {
		linkName := data.GetByPath("links.out.names").ArrayElement(i).AsStringDefault("")
		toId := data.GetByPath("links.out.ids").ArrayElement(i).AsStringDefault("")
		objectsOutLinks[toId] = linkName

		tp := data.GetByPath("links.out.types").ArrayElement(i).AsStringDefault("")
		if tp == crud.TO_TYPELINK {
			objectType = toId
		}
	}
	if len(objectType) == 0 {
		errResponse(ctx, "missing object type")
		return
	}

	for i := 0; i < data.GetByPath("links.in").ArraySize(); i++ {
		objectID := data.GetByPath("links.in").ArrayElement(i).GetByPath("from").AsStringDefault("")
		linkName := data.GetByPath("links.in").ArrayElement(i).GetByPath("name").AsStringDefault("")
		objectsOutLinks[objectID] = linkName
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

		typeBody, err := db.CMDB.TypeRead(current.ID)
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

				sort.Slice(node.Objects, func(i, j int) bool {
					return node.Objects[i].ID < node.Objects[j].ID
				})
			}
		}

		nodes = append(nodes, node)

		if current.Depth >= maxRadius {
			continue
		}

		outTypes, _ := typeBody.GetByPath("to_types").AsArrayString()

		for _, ioType := range inOutTypes(ctx, current.ID) {
			tbody, err := db.CMDB.TypeRead(ioType)
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

	sort.Slice(links, func(i, j int) bool {
		return links[i].Source < links[j].Source
	})

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Depth < nodes[j].Depth
	})

	nav := &typesNav{
		Links: links,
		Nodes: nodes,
	}

	okResponse(ctx, nav)
}

func inOutTypes(ctx *sf.StatefunContextProcessor, id string) []string {
	db := common.MustDBClient(ctx.Request)
	list := []string{}

	data, err := db.Graph.VertexRead(id)
	if err == nil {
		for i := 0; i < data.GetByPath("links.in").ArraySize(); i++ {
			objectID := data.GetByPath("links.in").ArrayElement(i).GetByPath("from").AsStringDefault("")

			link, err := db.CMDB.TypesLinkRead(objectID, id)
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

		for i := 0; i < data.GetByPath("links.out.names").ArraySize(); i++ {
			linkType := data.GetByPath("links.out.types").ArrayElement(i).AsStringDefault("")
			if crud.TO_TYPELINK != linkType {
				continue
			}
			toId := data.GetByPath("links.out.ids").ArrayElement(i).AsStringDefault("")
			list = append(list, toId)
		}
	} else {
		errResponse(ctx, "failed to read object")
	}

	return list
}
