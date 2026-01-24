package main

import (
	"strings"

	"github.com/google/uuid"
)

func newID() string {
	value := uuid.NewString()
	return strings.ReplaceAll(value, "-", "")
}

