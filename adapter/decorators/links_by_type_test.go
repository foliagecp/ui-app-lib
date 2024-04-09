package decorators

import (
	"testing"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	"github.com/foliagecp/sdk/statefun"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/sdk/statefun/test"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
	"github.com/stretchr/testify/suite"
)

type linksByTypeTestSuite struct {
	test.StatefunTestSuite
}

func TestLinksByTypeTestSuite(t *testing.T) {
	suite.Run(t, new(linksByTypeTestSuite))
}

func (s *linksByTypeTestSuite) Test() {
	typename := inStatefun.LINKS_TYPE_DECORATOR

	crud.RegisterAllFunctionTypes(s.Runtime())

	cfg := *statefun.NewFunctionTypeConfig().SetAllowedRequestProviders(sf.AutoRequestSelect)
	s.RegisterFunction(typename, linksByType, cfg)

	err := s.StartRuntime()
	s.Require().NoError(err)

	// prepare data
	err = fillTestData(s.Request)
	s.Require().NoError(err)
	//--------------

	objectID := "node_1"
	payload := easyjson.NewJSONObject()
	payload.SetByPath("link_type", easyjson.NewJSON("node_disk"))

	result, err := s.Request(sf.GolangLocalRequest, typename, objectID, &payload, nil)
	s.Require().NoError(err)

	wantStatus := "ok"
	s.Equal(wantStatus, result.GetByPath("status").AsStringDefault(""))

	wantData := `[{"source":"hub/node_1","target":"hub/disk_1","type":"node_disk"},{"source":"hub/node_1","target":"hub/disk_2","type":"node_disk"}]`
	s.JSONEq(wantData, result.GetByPath("data").ToString())
}
