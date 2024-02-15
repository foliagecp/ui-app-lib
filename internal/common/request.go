package common

import (
	"errors"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/embedded/graph/common"
	sf "github.com/foliagecp/sdk/statefun/plugins"
)

func ReplyOk(ctx *sf.StatefunContextProcessor) {
	Reply(ctx, "ok", easyjson.NewJSONObject())
}

func ReplyError(ctx *sf.StatefunContextProcessor, err error) {
	Reply(ctx, "failed", easyjson.NewJSON(err.Error()))
}

func CheckRequestError(result *easyjson.JSON, err error) error {
	if err != nil {
		return err
	}

	if result.GetByPath("payload.status").AsStringDefault("failed") == "failed" {
		return errors.New(result.GetByPath("payload.result").AsStringDefault("unknown error"))
	}

	return nil
}

func Reply(ctx *sf.StatefunContextProcessor, status string, data easyjson.JSON) {
	qid := common.GetQueryID(ctx)
	reply := easyjson.NewJSONObject()
	reply.SetByPath("status", easyjson.NewJSON(status))
	reply.SetByPath("result", data)
	common.ReplyQueryID(qid, easyjson.NewJSONObjectWithKeyValue("payload", reply).GetPtr(), ctx)
}
