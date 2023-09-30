//go:build !go1.16
// +build !go1.16

package io

import (
	"io/ioutil"
)

func init() {
	Discard = ioutil.Discard
	NopCloser = ioutil.NopCloser
}
