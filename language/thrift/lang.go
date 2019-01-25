// Package thrift provides support for thrift rules. It generates thrift_library
// rules only (not language-specific implementations).
package thrift

import (
	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/language"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

const thriftName = "thrift"

type thriftLang struct{}

func (_ *thriftLang) Name() string { return thriftName }

func NewLanguage() language.Language {
	return &thriftLang{}
}

func (_ *thriftLang) Fix(c *config.Config, f *rule.File) {}
