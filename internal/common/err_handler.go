package common

import "strings"

func ErrorAlreadyExists(err error) bool {
	return strings.Contains(err.Error(), "already exists")
}
