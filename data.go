// Package themetender holds the data that ships with the binary until the
// signed data bundle (M1 work package 12) replaces it: the app registry and
// the templates.
package themetender

import "embed"

// Data holds registry/schema.json, registry/apps/*.yaml and templates/.
//
//go:embed registry/schema.json registry/apps/*.yaml templates
var Data embed.FS
