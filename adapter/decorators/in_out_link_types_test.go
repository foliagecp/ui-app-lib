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

type inOutLinkTypesTestSuite struct {
	test.StatefunTestSuite
}

func TestInOutLinkTypesTestSuite(t *testing.T) {
	suite.Run(t, new(inOutLinkTypesTestSuite))
}

func (s *inOutLinkTypesTestSuite) Test() {
	typename := inStatefun.IO_LINK_TYPES_DECORATOR

	crud.RegisterAllFunctionTypes(s.Runtime())

	cfg := *statefun.NewFunctionTypeConfig().SetAllowedRequestProviders(sf.AutoRequestSelect)
	s.RegisterFunction(typename, inOutLinkTypes, cfg)

	err := s.StartRuntime()
	s.Require().NoError(err)

	// prepare data
	err = fillTestData(s.Request)
	s.Require().NoError(err)
	//--------------

	objectID := "node_1"
	payload := easyjson.NewJSONObject()

	result, err := s.Request(sf.GolangLocalRequest, typename, objectID, &payload, nil)
	s.Require().NoError(err)

	wantStatus := "ok"
	s.Equal(wantStatus, result.GetByPath("status").AsStringDefault(""))

	wantData := `{"in":["rack_node"],"out":["node_disk"]}`
	s.Equal(wantData, result.GetByPath("data").ToString())
}
