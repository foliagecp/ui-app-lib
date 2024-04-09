package decorators

import (
	"encoding/json"
	"testing"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	"github.com/foliagecp/sdk/statefun"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/sdk/statefun/test"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
	"github.com/stretchr/testify/suite"
)

type typesNavigationTestSuite struct {
	test.StatefunTestSuite
}

func TestTypesNavigationTestSuite(t *testing.T) {
	suite.Run(t, new(typesNavigationTestSuite))
}

func (s *typesNavigationTestSuite) Test() {
	typename := inStatefun.TYPES_NAVIGATION_DECORATOR

	crud.RegisterAllFunctionTypes(s.Runtime())

	cfg := *statefun.NewFunctionTypeConfig().SetAllowedRequestProviders(sf.AutoRequestSelect)
	s.RegisterFunction(typename, typesNavigation, cfg)

	err := s.StartRuntime()
	s.Require().NoError(err)

	// prepare data
	err = fillTestData(s.Request)
	s.Require().NoError(err)
	//--------------

	objectID := "node_1"
	payload := easyjson.NewJSONObject()
	payload.SetByPath("radius", easyjson.NewJSON(-1))

	result, err := s.Request(sf.GolangLocalRequest, typename, objectID, &payload, nil)
	s.Require().NoError(err)

	wantStatus := "ok"
	s.Require().Equal(wantStatus, result.GetByPath("status").AsStringDefault(""))

	wantNavigation := typesNav{
		Nodes: []routeNode{
			{
				ID:   "hub/node",
				Name: "test_node",
				Pos:  []int{1, 1},
			},
			{
				ID:   "hub/rack",
				Name: "test_rack",
				Pos:  []int{0, 1},
				Objects: []routeObject{
					{
						ID:   "hub/rack_1",
						Name: "to_node_1",
					},
				},
			},
			{
				ID:   "hub/disk",
				Name: "test_disk",
				Pos:  []int{2, 1},
				Objects: []routeObject{
					{
						ID:   "hub/disk_1",
						Name: "to_disk_1",
					},
					{
						ID:   "hub/disk_2",
						Name: "to_disk_2",
					},
				},
			},
		},
		Links: []link{
			{
				Source: "hub/rack",
				Target: "hub/node",
			},
			{
				Source: "hub/node",
				Target: "hub/disk",
			},
		},
	}

	wantNavigationJSON, err := json.Marshal(&wantNavigation)
	s.Require().NoError(err)

	gotNavigationJSON := result.GetByPath("data").ToString()

	s.JSONEq(string(wantNavigationJSON), gotNavigationJSON)
}
