package decorators

import (
	"fmt"
	"testing"
	"time"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/clients/go/db"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	"github.com/foliagecp/sdk/statefun"
	sf "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/sdk/statefun/test"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/suite"
)

type childrenByLinkTypeTestSuite struct {
	test.StatefunTestSuite
}

func TestChildrenByLinkTypeTestSuite(t *testing.T) {
	suite.Run(t, new(childrenByLinkTypeTestSuite))
}

func (s *childrenByLinkTypeTestSuite) Test() {
	typename := inStatefun.CHILDREN_LINK_TYPE_DECORATOR

	crud.RegisterAllFunctionTypes(s.Runtime())

	cfg := *statefun.NewFunctionTypeConfig().SetAllowedRequestProviders(sf.AutoRequestSelect)
	s.RegisterFunction(typename, childrenUUIDsByLinkType, cfg)

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

	wantData := `["hub/disk_1","hub/disk_2"]`
	s.Equal(wantData, result.GetByPath("data").ToString())
}

func (s *childrenByLinkTypeTestSuite) Test_External() {
	nc, err := nats.Connect("nats://nats:foliage@127.0.0.1:4222")
	s.Require().NoError(err)

	subj := fmt.Sprintf(`request.hub.%s.%s`, inStatefun.CHILDREN_LINK_TYPE_DECORATOR, "hub/152398c7-3bd6-562e-a708-37913053b1fb")
	data := []byte(`{"payload":{"link_type":"scala_hlm_service-service"}}`)
	msg, err := nc.Request(subj, data, 5*time.Second)
	s.Require().NoError(err)

	fmt.Printf("msg.Data: %s\n", msg.Data)
}

func fillTestData(request sf.SFRequestFunc) error {
	cmdb, err := db.NewCMDBSyncClientFromRequestFunction(request)
	if err != nil {
		return err
	}

	if err := cmdb.TypeCreate("node", newTestViewNavigationBody("test_node", [2]int{1, 1})); err != nil {
		return err
	}

	if err := cmdb.TypeCreate("disk", newTestViewNavigationBody("test_disk", [2]int{2, 1})); err != nil {
		return err
	}

	if err := cmdb.TypeCreate("rack", newTestViewNavigationBody("test_rack", [2]int{0, 1})); err != nil {
		return err
	}

	if err := cmdb.TypesLinkCreate("rack", "node", "rack_node", []string{}); err != nil {
		return err
	}

	if err := cmdb.TypesLinkCreate("node", "disk", "node_disk", []string{}); err != nil {
		return err
	}

	if err := cmdb.ObjectCreate("node_1", "node"); err != nil {
		return err
	}

	if err := cmdb.ObjectCreate("disk_1", "disk"); err != nil {
		return err
	}

	if err := cmdb.ObjectCreate("disk_2", "disk"); err != nil {
		return err
	}

	if err := cmdb.ObjectCreate("rack_1", "rack"); err != nil {
		return err
	}

	if err := cmdb.ObjectsLinkCreate("rack_1", "node_1", "to_node_1", []string{}); err != nil {
		return err
	}

	if err := cmdb.ObjectsLinkCreate("node_1", "disk_1", "to_disk_1", []string{}); err != nil {
		return err
	}

	if err := cmdb.ObjectsLinkCreate("node_1", "disk_2", "to_disk_2", []string{}); err != nil {
		return err
	}

	return nil
}

func newTestViewNavigationBody(alias string, pos [2]int) easyjson.JSON {
	body := easyjson.NewJSONObject()
	body.SetByPath("view_navigation", easyjson.NewJSON(true))
	body.SetByPath("alias", easyjson.NewJSON(alias))
	body.SetByPath("pos", easyjson.NewJSON(pos))
	return body
}
