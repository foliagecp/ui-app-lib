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

	in := make([]string, 0)
	inPattern := common.InLinkKeyPattern(ctx.Self.ID, ">")

	for _, key := range ctx.Domain.Cache().GetKeysByPattern(inPattern) {
		split := strings.Split(key, ".")
		if len(split) == 0 {
			continue
		}

		objectID := split[len(split)-2]

		linkType, err := common.ObjectType(c, objectID)
		if err != nil {
			continue
		}

		linkType = ctx.Domain.GetObjectIDWithoutDomain(linkType)
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
		out = append(out, linkType)
	}

	resp := easyjson.NewJSONObject()
	resp.SetByPath("in", easyjson.JSONFromArray(in))
	resp.SetByPath("out", easyjson.JSONFromArray(out))
	okResponse(ctx, resp)
}
