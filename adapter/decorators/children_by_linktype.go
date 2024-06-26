package decorators

import (
	"sort"

	"github.com/foliagecp/sdk/statefun/logger"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/ui-app-lib/internal/common"
)

/*
	Request: {
		"link_type"
	}

	Response: {
		"status"
		"message"
		"data": []string
	}
*/
func childrenUUIDsByLinkType(_ sf.StatefunExecutor, ctx *sf.StatefunContextProcessor) {
	filterLinkType, ok := ctx.Payload.GetByPath("link_type").AsString()
	if !ok {
		errResponse(ctx, "missing link_type")
		return
	}

	db := common.MustDBClient(ctx.Request)

	data, err := db.Graph.VertexRead(ctx.Self.ID)
	if err != nil {
		logger.Logln(logger.ErrorLevel, err.Error())
		return
	}

	result := []string{}
	for i := 0; i < data.GetByPath("links.out.names").ArraySize(); i++ {
		tp := data.GetByPath("links.out.types").ArrayElement(i).AsStringDefault("")
		toId := data.GetByPath("links.out.ids").ArrayElement(i).AsStringDefault("")
		if tp == filterLinkType {
			result = append(result, toId)
		}
	}
	sort.Strings(result)

	okResponse(ctx, result)
}
