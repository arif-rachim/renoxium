//go:build !linux

package sandbox

import "errors"

func restrictWorker() error { return errors.New("the worker sandbox needs Linux") }
