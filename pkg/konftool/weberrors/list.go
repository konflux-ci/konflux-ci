package weberrors

import (
	"errors"
)

type List []error

// Add an error to the list if its not nil. Returns e
func (l *List) Add(e error) error {
	if e != nil {
		*l = append(*l, e)
	}
	return e
}

// Adds an error to the list if its not nil and errors.Is(e, otherE) is false
func (l *List) AddIfNot(e, otherE error) error {
	if e != nil && errors.Is(e, otherE) {
		return e
	}
	return l.Add(e)
}

// Add an error to the list, but only if no other error in the list returns
// true for errors.Is(e, otherE) and e is not nil. Returns e.
func (l *List) AddOnce(e error) error {
	if e != nil {
		for _, otherE := range *l {
			if errors.Is(e, otherE) {
				return e
			}
		}
	}
	return l.Add(e)
}

// The combination of AddOnce and AddIfNot
func (l *List) AddOnceIfNot(e, otherE error) error {
	if e != nil && errors.Is(e, otherE) {
		return e
	}
	return l.AddOnce(e)
}
