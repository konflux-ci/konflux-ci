package webapps

import (
	"errors"
	"fmt"

	"github.com/konflux-ci/konflux-ci/pkg/konftool/weberrors"
)

// Interface for providing key/value store services to apps
type Store interface {
	Get(key string, value interface{}) error
	Put(key string, value interface{}) error	
}

var (
	// Error value indicating a key was not found in the store.
	// When a key is not found, implenetiotions should return an error `e` such
	// that errors.Is(e, ErrKeyNotFound) returns true.
	ErrKeyNotFound = errors.New("key not found")

	// Error value indicating the store could not be read.
	// Implementations shuld return all reading errors such that 
	// error.Is(e, ErrCantReadStore) is true as well as
	// error.Is(e1, e2) is true for all reading errors.
	// This is so we can try to display only a single error to the user acress
	// multiple attempts to access the store.
	ErrCantReadStore = errors.New("faild to read configration store")

	// Error value indicating the store could not be written to.
	// Implementations shuld return all writing errors such that 
	// error.Is(e, ErrCantWriteToStore) is true as well as
	// error.Is(e1, e2) is true for all writing errors.
	// This is so we can try to display only a single error to the user acress
	// multiple attempts to write to the store.
	ErrCantWriteToStore = errors.New("faild to write to configration store")
)

// Convenience wrapper for generating ErrKeyNotFound errors
func KeyNotFound(key string) error {
	return fmt.Errorf("%w: %v", ErrKeyNotFound, key)
}

// Convenience wrapper for generating ErrCantReadStore errors
// If the given error is nil, nil will be returned rather then an error value
func CantReadStoreErr(err error) error {
	if err == nil {
		return nil
	}
	return weberrors.SubError(ErrCantReadStore, err)
}

// Convenience wrapper for generating ErrCantWriteToStore errors
// If the given error is nil, nil will be returned rather then an error value
func CantWriteToStoreErr(err error) error {
	if err == nil {
		return nil
	}
	return weberrors.SubError(ErrCantWriteToStore, err)
}
