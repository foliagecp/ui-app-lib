package decorators

import (
	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/statefun"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
)

func Register(r *statefun.Runtime) {
	statefun.NewFunctionType(r, inStatefun.TYPES_NAVIGATION_DECORATOR, typesNavigation, *statefun.NewFunctionTypeConfig().SetAllowedRequestProviders(sf.AutoRequestSelect).SetMaxIdHandlers(-1))
	statefun.NewFunctionType(r, inStatefun.IO_LINK_TYPES_DECORATOR, inOutLinkTypes, *statefun.NewFunctionTypeConfig().SetAllowedRequestProviders(sf.AutoRequestSelect).SetMaxIdHandlers(-1))
	statefun.NewFunctionType(r, inStatefun.LINKS_TYPE_DECORATOR, linksByType, *statefun.NewFunctionTypeConfig().SetAllowedRequestProviders(sf.AutoRequestSelect).SetMaxIdHandlers(-1))
}

func errResponse(ctx *sf.StatefunContextProcessor, msg string) {
	resp := easyjson.NewJSONObject()
	resp.SetByPath("status", easyjson.NewJSON("failed"))
	resp.SetByPath("message", easyjson.NewJSON(msg))
	ctx.Reply.With(resp.GetPtr())
}

func okResponse(ctx *sf.StatefunContextProcessor, data any) {
	resp := easyjson.NewJSONObject()
	resp.SetByPath("status", easyjson.NewJSON("ok"))
	if j, ok := data.(easyjson.JSON); ok {
		resp.SetByPath("data", j)
	} else {
		resp.SetByPath("data", easyjson.NewJSON(data))
	}
	ctx.Reply.With(resp.GetPtr())
}
