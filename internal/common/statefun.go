package common

import (
	"github.com/foliagecp/easyjson"
	sf "github.com/foliagecp/sdk/statefun/plugins"
)

type StatefunContext struct {
	*sf.StatefunContextProcessor
}

func NewStatefunContext(ctx *sf.StatefunContextProcessor) *StatefunContext {
	return &StatefunContext{
		StatefunContextProcessor: ctx,
	}
}

func (c *StatefunContext) Request(requestProvider sf.RequestProvider, typename, id string, payload, options *easyjson.JSON) (*easyjson.JSON, error) {
	return c.StatefunContextProcessor.Request(requestProvider, typename, id, payload, options)
}
