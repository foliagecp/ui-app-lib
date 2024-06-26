package decorators

import (
	"strings"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/statefun/logger"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/ui-app-lib/internal/common"
)

/*
Request: {}

	Response: {
		"status"
		"message"
		"data": {
			"in":	[]string,
			"out":  []string
		}
	}
*/
func inOutLinkTypes(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	db := common.MustDBClient(ctx.Request)
	visited := make(map[string]struct{})

	data, err := db.Graph.VertexRead(ctx.Self.ID)
	if err != nil {
		logger.Logln(logger.ErrorLevel, err.Error())
		return
	}

	in := []string{}
	for i := 0; i < data.GetByPath("links.in").ArraySize(); i++ {
		objectID := data.GetByPath("links.in").ArrayElement(i).GetByPath("from").AsStringDefault("")

		linkBody, err := db.CMDB.ObjectsLinkRead(objectID, ctx.Self.ID)
		if err != nil {
			continue
		}

		linkType, ok := linkBody.GetByPath("type").AsString()
		if !ok {
			continue
		}

		if !filterLinkType(linkType) {
			continue
		}

		if _, ok := visited[linkType]; ok {
			continue
		}

		visited[linkType] = struct{}{}

		in = append(in, linkType)
	}

	out := []string{}
	for i := 0; i < data.GetByPath("links.out.names").ArraySize(); i++ {
		linkType := data.GetByPath("links.out.types").ArrayElement(i).AsStringDefault("")

		if !filterLinkType(linkType) {
			continue
		}

		if _, ok := visited[linkType]; ok {
			continue
		}

		visited[linkType] = struct{}{}

		out = append(out, linkType)
	}

	resp := easyjson.NewJSONObject()
	resp.SetByPath("in", easyjson.JSONFromArray(in))
	resp.SetByPath("out", easyjson.JSONFromArray(out))
	okResponse(ctx, resp)
}

func filterLinkType(lt string) bool {
	// it means that certain link type related with internal
	if strings.HasPrefix(lt, "__") {
		return false
	}

	// TODO: add more filters for crud link type

	return true
}
