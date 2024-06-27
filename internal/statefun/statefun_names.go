package statefun

const (
	INGRESS                  = "ingress.ui"
	SESSION_ROUTER           = "functions.ui.app.session.router"
	SESSION_START            = "functions.ui.app.session.start"
	SESSION_CLOSE            = "functions.ui.app.session.close"
	SESSION_WATCH            = "functions.ui.app.session.watch"
	SESSION_UPDATE_ACTIVITY  = "functions.ui.app.session.update.activity"
	SESSION_START_CONTROLLER = "functions.ui.app.session.controller.start"
	SESSION_CLEAR_CONTROLLER = "functions.ui.app.session.controller.clear"
	EGRESS                   = "ui"

	CONTROLLER_START          = "functions.ui.app.controller.start"
	CONTROLLER_CLEAR          = "functions.ui.app.controller.clear"
	CONTROLLER_OBJECT_UPDATE  = "functions.ui.app.controller.object.update"
	CONTROLLER_CONSTRUCT      = "functions.ui.app.controller.construct"
	CONTROLLER_OBJECT_TRIGGER = "functions.ui.app.controller.object.trigger"

	TYPES_NAVIGATION_DECORATOR   = "functions.ui.app.decorator.types.navigation"
	IO_LINK_TYPES_DECORATOR      = "functions.ui.app.decorator.types.link.io"
	CHILDREN_LINK_TYPE_DECORATOR = "functions.ui.app.decorator.type.link.children"
	LINKS_TYPE_DECORATOR         = "functions.ui.app.decorator.type.links"
)
