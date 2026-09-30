package main

import (
	"strings"
	"testing"
)

func TestProductTaskProjectionKeepsSourceAuthorityPrivate(t *testing.T) {
	projection := generateProjection(productRoots()...)
	for _, expected := range []string{"export interface WorkTarget {", "export interface TaskStart {", "export interface TaskPreview {", "export interface WorkTargetInfo {", "target?: WorkTarget | null;", "targetLabel?: string;"} {
		if !strings.Contains(projection, expected) {
			t.Fatalf("missing consumed task contract %q", expected)
		}
	}
	for _, private := range []string{"WorkDispatchSource", "RequestDigest", "BindingID", "OperationID", "Source:"} {
		if strings.Contains(projection, private) {
			t.Fatalf("host-only dispatch field leaked: %s", private)
		}
	}
}

type privatePortProjection struct {
	Visible string `json:"visible"`
	Host    func() `json:"-"`
}

func TestIgnoredHostPortIsNotTraversed(t *testing.T) {
	// A function is deliberately unsupported in wire schemas. An ignored host
	// port must be excluded before traversal as well as before field emission.
	projection := generateProjection(privatePortProjection{})
	if !strings.Contains(projection, "visible: string;") || strings.Contains(projection, "Host") {
		t.Fatal(projection)
	}
}
