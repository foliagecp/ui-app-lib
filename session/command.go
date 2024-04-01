package session

type Command string

const (
	START_SESSION    Command = "START_SESSION"
	CLOSE_SESSION    Command = "CLOSE_SESSION"
	START_CONTROLLER Command = "START_CONTROLLER"
	CLEAR_CONTROLLER Command = "CLEAR_CONTROLLER"
)
