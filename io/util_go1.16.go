//go:build go1.16
// +build go1.16

package io

import "io"

func init() {
	Discard = io.Discard
	NopCloser = io.NopCloser
}
