package decorators

import (
	"strings"

	"github.com/foliagecp/easyjson"
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
	c := common.MustCMDBClient(ctx.Request)
	visited := make(map[string]struct{})

	in := make([]string, 0)
	inPattern := common.InLinkKeyPattern(ctx.Self.ID, ">")

	for _, key := range ctx.Domain.Cache().GetKeysByPattern(inPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		objectID := split[len(split)-2]

		linkBody, err := c.ObjectsLinkRead(objectID, ctx.Self.ID)
		if err != nil {
			continue
		}

		linkType, ok := linkBody.GetByPath("type").AsString()
		if !ok {
			continue
		}

		if _, ok := visited[linkType]; ok {
			continue
		}

		visited[linkType] = struct{}{}

		in = append(in, linkType)
	}

	out := make([]string, 0)
	outPattern := common.OutLinkType(ctx.Self.ID, ">")

	for _, key := range ctx.Domain.Cache().GetKeysByPattern(outPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		linkType := split[len(split)-2]

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
