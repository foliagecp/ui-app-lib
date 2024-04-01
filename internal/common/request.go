package common

import (
	"fmt"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/clients/go/db"
	sf "github.com/foliagecp/sdk/statefun/plugins"
)

func ObjectType(c db.CMDBSyncClient, id string) (string, error) {
	objectBody, err := c.ObjectRead(id)
	if err != nil {
		return "", fmt.Errorf("failed to find uuid type: %w", err)
	}

	objectType, ok := objectBody.GetByPath("type").AsString()
	if !ok {
		return "", fmt.Errorf("object's type not defined")
	}

	return objectType, nil
}

func Reply(ctx *sf.StatefunContextProcessor, status string, data easyjson.JSON) {
	reply := easyjson.NewJSONObject()
	reply.SetByPath("status", easyjson.NewJSON(status))
	reply.SetByPath("result", data)
	ctx.Reply.With(&reply)
}
