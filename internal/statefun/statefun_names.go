package statefun

const (
	INGRESS         = "ui.ingress"
	SESSION_CONTROL = "functions.ui.app.session"
	PREPARE_EGRESS  = "functions.ui.app.prepare.egress"
	EGRESS          = "ui.egress"

	CONTROLLER_SETUP          = "functions.ui.app.controller.setup"
	CONTROLLER_UNSUB          = "functions.ui.app.controller.unsub"
	CONTROLLER_CONSTRUCT      = "functions.ui.app.controller.construct"
	CONTROLLER_RESULT_COMPARE = "functions.ui.app.controller.result.compare"
	CONTROLLER_UPDATE         = "functions.ui.app.controller.update"
	CONTROLLER_TRIGGER        = "functions.ui.app.controller.trigger"
)
