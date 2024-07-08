package weberrors

import (
	"errors"
	"fmt"
)

// A sub-error is an error that has a proto-error so that:
//   errors.Is(sub, proto) == true
//   errors.Is(sub1, sub2) == true  // if they have the same proto error
// A sub error, also simply wrapes a nother "real" internal error
type subError struct {
	proto error
	sub error
}

func SubError(proto, sub error) error {
	return &subError{proto: proto, sub: sub}
}

func (e *subError) Error() string {
	return fmt.Sprintf("%v: %v", e.proto, e.sub)
}

func (e *subError) Is(otherE error) bool {
	return errors.Is(otherE, e.proto)
}

func (e *subError) Unwrap() error {
	return e.sub
}
