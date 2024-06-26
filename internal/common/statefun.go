package common

import (
	"fmt"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/clients/go/db"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	"github.com/foliagecp/sdk/statefun/logger"
	"github.com/foliagecp/sdk/statefun/plugins"
	sf "github.com/foliagecp/sdk/statefun/plugins"
)

func GetRemoteContext(ctx *sf.StatefunContextProcessor) *easyjson.JSON {
	emptyData := easyjson.NewJSONObject().GetPtr()
	db := MustDBClient(ctx.Request)

	data, err := db.Graph.VertexRead(ctx.Self.ID, true)
	if err != nil {
		logger.Logln(logger.ErrorLevel, err.Error())
		return emptyData
	}
	body := data.GetByPathPtr("body")
	if body.IsNonEmptyObject() {
		return data.GetByPathPtr("body")
	}
	return emptyData
}

func MustDBClient(request plugins.SFRequestFunc) db.DBSyncClient {
	c, err := db.NewDBSyncClientFromRequestFunction(request)
	if err != nil {
		panic(err)
	}
	return c
}

func OutTargetLink(source, linkname string) string {
	return fmt.Sprintf(crud.OutLinkTargetKeyPrefPattern+crud.LinkKeySuff1Pattern, source, linkname)
}

func OutLinkType(source, ltype string, target ...string) string {
	if len(target) > 0 {
		return fmt.Sprintf(crud.OutLinkTypeKeyPrefPattern+crud.LinkKeySuff2Pattern, source, ltype, target[0])
	}
	return fmt.Sprintf(crud.OutLinkTypeKeyPrefPattern+crud.LinkKeySuff1Pattern, source, ltype)
}

func InLinkKeyPattern(id, target string, linkType ...string) string {
	if len(linkType) > 0 {
		lt := linkType[0]

		return fmt.Sprintf(crud.InLinkKeyPrefPattern+crud.LinkKeySuff2Pattern,
			id, target, lt,
		)
	}

	return fmt.Sprintf(crud.InLinkKeyPrefPattern+crud.LinkKeySuff1Pattern,
		id, target,
	)
}
