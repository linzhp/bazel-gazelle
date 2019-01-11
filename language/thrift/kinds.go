package thrift

import "github.com/bazelbuild/bazel-gazelle/rule"

var thriftKinds = map[string]rule.KindInfo{
	"thrift_library": {
		NonEmptyAttrs:  map[string]bool{"src": true},
		MergeableAttrs: map[string]bool{"src": true},
		ResolveAttrs:   map[string]bool{"deps": true},
	},
}

var thriftLoads = []rule.LoadInfo{
	{
		Name: "//tools/codegen:thrift.bzl",
		Symbols: []string{
			"thrift_library",
		},
	},
}

func (_ *thriftLang) Kinds() map[string]rule.KindInfo { return thriftKinds }
func (_ *thriftLang) Loads() []rule.LoadInfo          { return thriftLoads }
