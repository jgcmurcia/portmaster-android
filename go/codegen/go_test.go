package main

import (
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedProxyWithMultipleResultsIsValidGo(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "proxy.go")
	function := Func{Name: "Results", CreateProxy: true, ReturnTypes: []string{"string", "error", "bool"}}
	writeToGoFile(filename, []Func{function})
	source, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := format.Source(source); err != nil {
		t.Fatalf("generated proxy is invalid Go: %v\n%s", err, source)
	}
	if !strings.Contains(string(source), `"ret2": ret2`) {
		t.Fatal("Go response must preserve result indexes around errors")
	}
	if got := getTypescriptReturnNames("result.", function); got != "[result.ret0, result.ret2]" {
		t.Fatalf("TypeScript reads mismatched result fields: %s", got)
	}
}
