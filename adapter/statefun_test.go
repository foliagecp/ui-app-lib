package adapter_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/clients/go/db"
	"github.com/foliagecp/sdk/embedded/graph/crud"
	"github.com/foliagecp/sdk/statefun"
	sfplugins "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/sdk/statefun/test"
	"github.com/foliagecp/ui-app-lib/adapter"
	"github.com/foliagecp/ui-app-lib/internal/generate"
	inStatefun "github.com/foliagecp/ui-app-lib/internal/statefun"
	"github.com/foliagecp/ui-app-lib/session"
	"github.com/stretchr/testify/suite"
)

type adapterTestSuite struct {
	test.StatefunTestSuite
}

func TestAdapterTestSuite(t *testing.T) {
	suite.Run(t, new(adapterTestSuite))
}

func (s *adapterTestSuite) Test_InitSchema() {
	crud.RegisterAllFunctionTypes(s.Runtime())
	session.RegisterFunctions(s.Runtime())

	s.OnAfterStartFunction(adapter.InitSchema, true)

	err := s.StartRuntime()
	s.NoError(err)

	_, err = s.CacheValue(inStatefun.CONTROLLER_TYPE)
	s.NoError(err)

	_, err = s.CacheValue(inStatefun.CONTROLLER_OBJECT_TYPE)
	s.NoError(err)

	// TODO: check types link
}

func (s *adapterTestSuite) Test_StartController_Correct() {
	typename := inStatefun.CONTROLLER_START

	// register related statefun
	crud.RegisterAllFunctionTypes(s.Runtime())
	session.RegisterFunctions(s.Runtime())
	s.RegisterFunction(inStatefun.CONTROLLER_OBJECT_TRIGGER, adapter.UpdateControllerObject, *statefun.NewFunctionTypeConfig())
	s.OnAfterStartFunction(adapter.InitSchema, true)
	// -------------------------

	s.RegisterFunction(typename, adapter.StartController, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))

	err := s.StartRuntime()
	s.Require().NoError(err)

	cmdb, err := db.NewCMDBSyncClientFromRequestFunction(s.Request)
	s.Require().NoError(err)

	sessionID := "signal"
	err = cmdb.ObjectCreate(sessionID, inStatefun.SESSION_TYPE)
	s.Require().NoError(err)

	controllerName := "test_controller"
	controllerID := generate.UUID(controllerName).String()
	controllerDeclaration := easyjson.NewJSONObjectWithKeyValue("props", easyjson.NewJSON("@property:"))
	uuids := []string{"uuid_1", "uuid_2"}

	err = cmdb.TypeCreate("test_uuid")
	s.Require().NoError(err)

	for _, v := range uuids {
		cmdb.ObjectCreate(v, "test_uuid", easyjson.NewJSONObjectWithKeyValue("key", easyjson.NewJSON("value")))
	}

	payload := easyjson.NewJSONObject()
	payload.SetByPath("name", easyjson.NewJSON(controllerName))
	payload.SetByPath("declaration", controllerDeclaration)
	payload.SetByPath("uuids", easyjson.JSONFromArray(uuids))

	err = s.Signal(sfplugins.AutoSignalSelect, typename, controllerID, &payload, nil)
	s.Require().NoError(err)

	time.Sleep(1 * time.Second)

	// check controller
	controllerBody, err := s.CacheValue(controllerID)
	s.Require().NoError(err)

	s.JSONEq(controllerDeclaration.ToString(), controllerBody.GetByPath("declaration").ToString())
	s.Equal(controllerName, controllerBody.GetByPath("name").AsStringDefault(""))

	// check controller objects
	controllerIDWithDomain := s.SetThisDomainPreffix(controllerID)
	for _, v := range uuids {
		ctrlObjectID := generate.UUID(controllerIDWithDomain + v).String()
		body, err := s.CacheValue(ctrlObjectID)
		s.Require().NoError(err)

		s.Equal(controllerIDWithDomain, body.GetByPath("parent").AsStringDefault(""))
	}

	// check trigger
	typeBody, err := s.CacheValue("test_uuid")
	s.Require().NoError(err)

	wantTriggers := fmt.Sprintf(`["%s"]`, inStatefun.CONTROLLER_OBJECT_TRIGGER)
	updateTriggers := typeBody.GetByPath("triggers.update").ToString()

	s.Equal(wantTriggers, updateTriggers)
}

func (s *adapterTestSuite) Test_ControllerObjectTrigger_Correct() {
	typename := inStatefun.CONTROLLER_OBJECT_TRIGGER

	crud.RegisterAllFunctionTypes(s.Runtime())
	s.RegisterFunction(inStatefun.CONTROLLER_OBJECT_UPDATE, adapter.UpdateControllerObject, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1))
	s.OnAfterStartFunction(adapter.InitSchema, true)

	s.RegisterFunction(typename, adapter.ControllerObjectTrigger, *statefun.NewFunctionTypeConfig())

	err := s.StartRuntime()
	s.Require().NoError(err)

	cmdb, err := db.NewCMDBSyncClientFromRequestFunction(s.Request)
	s.Require().NoError(err)

	cmdb.TypeCreate("uuid_type")
	cmdb.ObjectCreate("uuid_1", "uuid_type")
	cmdb.TypesLinkCreate(inStatefun.CONTROLLER_OBJECT_TYPE, "uuid_type", inStatefun.CONTROLLER_SUBJECT_TYPE, []string{})
	cmdb.TriggerObjectSet("uuid_type", db.UpdateTrigger, inStatefun.CONTROLLER_OBJECT_TRIGGER)

	cmdb.ObjectCreate("ctrl_object_1", inStatefun.CONTROLLER_OBJECT_TYPE)
	cmdb.ObjectsLinkCreate("ctrl_object_1", "uuid_1", "uuid_1", []string{})

	cmdb.ObjectUpdate("uuid_1", easyjson.NewJSONObjectWithKeyValue("key", easyjson.NewJSON("value")), true)

	time.Sleep(1 * time.Second)
}

func (s *adapterTestSuite) Test_ConstructController_Correct() {
	typename := inStatefun.CONTROLLER_CONSTRUCT

	crud.RegisterAllFunctionTypes(s.Runtime())
	s.RegisterFunction(typename, adapter.ControllerConstruct, *statefun.NewFunctionTypeConfig().SetMaxIdHandlers(-1).SetAllowedRequestProviders(sfplugins.AutoRequestSelect))

	err := s.StartRuntime()
	s.Require().NoError(err)

	cmdb, err := db.NewCMDBSyncClientFromRequestFunction(s.Request)
	s.Require().NoError(err)

	cmdb.TypeCreate("test_uuid")

	objectBody := easyjson.NewJSONObject()
	objectBody.SetByPath("key", easyjson.NewJSON("value"))

	err = cmdb.ObjectCreate("uuid_1", "test_uuid", objectBody)
	s.Require().NoError(err)

	payload := easyjson.NewJSONObject()
	payload.SetByPath("props", easyjson.NewJSON("@property:key"))

	result, err := s.Request(sfplugins.GolangLocalRequest, typename, "uuid_1", &payload, nil)
	s.Require().NoError(err)

	s.JSONEq(objectBody.GetByPath("key").ToString(), result.GetByPath("result.props").ToString())
}
