package secrets

import (
	"fmt"
)

type NotFoundError struct {
	message string
}

func (e NotFoundError) Error() string {
	return e.message
}

func (e NotFoundError) IsEveError() bool {
	return true
}

func NotFoundErrorf(format string, a ...interface{}) NotFoundError {
	return NotFoundError{
		message: fmt.Sprintf(format, a...),
	}
}
