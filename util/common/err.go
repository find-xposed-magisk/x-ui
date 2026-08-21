package common

import (
	"errors"
	"fmt"

	"github.com/alireza0/x-ui/logger"
)

func NewErrorf(format string, a ...interface{}) error {
	return fmt.Errorf(format, a...)
}

func NewError(a ...interface{}) error {
	return errors.New(fmt.Sprintln(a...))
}

func Recover(msg string) interface{} {
	panicErr := recover()
	if panicErr != nil {
		if msg != "" {
			logger.Error(msg, "panic:", panicErr)
		}
	}
	return panicErr
}
