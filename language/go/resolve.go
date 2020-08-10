/* Copyright 2018 The Bazel Authors. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package golang

import (
	"errors"
	"fmt"
	"go/build"
	"log"
	"path"
	"regexp"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/pathtools"
	"github.com/bazelbuild/bazel-gazelle/repo"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

func (_ *goLang) Imports(_ *config.Config, r *rule.Rule, f *rule.File) []resolve.ImportSpec {
	if !isGoLibrary(r.Kind()) || isExtraLibrary(r) {
		return nil
	}
	if importPath := r.AttrString("importpath"); importPath == "" {
		return []resolve.ImportSpec{}
	} else {
		return []resolve.ImportSpec{{
			Lang: goName,
			Imp:  importPath,
		}}
	}
}

func (_ *goLang) Embeds(r *rule.Rule, from label.Label) []label.Label {
	embedStrings := r.AttrStrings("embed")
	if isGoProtoLibrary(r.Kind()) {
		embedStrings = append(embedStrings, r.AttrString("proto"))
	}
	embedLabels := make([]label.Label, 0, len(embedStrings))
	for _, s := range embedStrings {
		l, err := label.Parse(s)
		if err != nil {
			continue
		}
		l = l.Abs(from.Repo, from.Pkg)
		embedLabels = append(embedLabels, l)
	}
	return embedLabels
}

func (gl *goLang) Resolve(c *config.Config, ix *resolve.RuleIndex, rc *repo.RemoteCache, r *rule.Rule, importsRaw interface{}, from label.Label) {
	if importsRaw == nil {
		// may not be set in tests.
		return
	}
	imports := importsRaw.(rule.PlatformStrings)
	r.DelAttr("deps")
	var resolve func(*config.Config, *resolve.RuleIndex, *repo.RemoteCache, string, label.Label) (label.Label, error)
	switch r.Kind() {
	case "go_proto_library":
		resolve = resolveProto
	case "go_tool_library":
		resolve = resolveGoTool
	default:
		resolve = ResolveGo
	}
	deps, errs := imports.Map(func(imp string) (string, error) {
		l, err := resolve(c, ix, rc, imp, from)
		if err == skipImportError {
			return "", nil
		} else if err != nil {
			return "", err
		}
		for _, embed := range gl.Embeds(r, from) {
			if embed.Equal(l) {
				return "", nil
			}
		}
		l = l.Rel(from.Repo, from.Pkg)
		return l.String(), nil
	})
	for _, err := range errs {
		log.Print(err)
	}
	if !deps.IsEmpty() {
		if r.Kind() == "go_proto_library" {
			// protos may import the same library multiple times by different names,
			// so we need to de-duplicate them. Protos are not platform-specific,
			// so it's safe to just flatten them.
			r.SetAttr("deps", deps.Flat())
		} else {
			r.SetAttr("deps", deps)
		}
	}
}

var (
	skipImportError = errors.New("std or self import")
	notFoundError   = errors.New("rule not found")
)

// ResolveGo resolves a Go import path to a Bazel label, possibly using the
// given rule index and remote cache. Some special cases may be applied to
// known proto import paths, depending on the current proto mode.
//
// This may be used directly by other language extensions related to Go
// (gomock). Gazelle calls Language.Resolve instead.
func ResolveGo(c *config.Config, ix *resolve.RuleIndex, rc *repo.RemoteCache, imp string, from label.Label) (label.Label, error) {
	gc := getGoConfig(c)
	if build.IsLocalImport(imp) {
		cleanRel := path.Clean(path.Join(from.Pkg, imp))
		if build.IsLocalImport(cleanRel) {
			return label.NoLabel, fmt.Errorf("relative import path %q from %q points outside of repository", imp, from.Pkg)
		}
		imp = path.Join(gc.prefix, cleanRel)
	}

	if IsStandard(imp) {
		return label.NoLabel, skipImportError
	}

	if l, ok := resolve.FindRuleWithOverride(c, resolve.ImportSpec{Lang: "go", Imp: imp}, "go"); ok {
		return l, nil
	}

	if l, err := resolveWithIndexGo(c, ix, imp, from); err == nil || err == skipImportError {
		return l, err
	} else if err != notFoundError {
		return label.NoLabel, err
	}

	// Special cases for rules_go and bazel_gazelle.
	// These have names that don't following conventions and they're
	// typeically declared with http_archive, not go_repository, so Gazelle
	// won't recognize them.
	if pathtools.HasPrefix(imp, "github.com/bazelbuild/rules_go") {
		pkg := pathtools.TrimPrefix(imp, "github.com/bazelbuild/rules_go")
		return label.New("io_bazel_rules_go", pkg, "go_default_library"), nil
	} else if pathtools.HasPrefix(imp, "github.com/bazelbuild/bazel-gazelle") {
		pkg := pathtools.TrimPrefix(imp, "github.com/bazelbuild/bazel-gazelle")
		return label.New("bazel_gazelle", pkg, "go_default_library"), nil
	}

	if !c.IndexLibraries {
		// packages in current repo were not indexed, relying on prefix to decide what may have been in
		// current repo
		if pathtools.HasPrefix(imp, gc.prefix) {
			pkg := path.Join(gc.prefixRel, pathtools.TrimPrefix(imp, gc.prefix))
			libName := libNameByConvention(gc.goNamingConvention, imp, "")
			return label.New("", pkg, libName), nil
		}
	}

	if gc.depMode == externalMode {
		return resolveExternal(c, rc, imp)
	} else {
		return resolveVendored(gc, imp)
	}
}

// IsStandard returns whether a package is in the standard library.
func IsStandard(imp string) bool {
	return stdPackages[imp]
}

func resolveWithIndexGo(c *config.Config, ix *resolve.RuleIndex, imp string, from label.Label) (label.Label, error) {
	matches := ix.FindRulesByImportWithConfig(c, resolve.ImportSpec{Lang: "go", Imp: imp}, "go")
	var bestMatch resolve.FindResult
	var bestMatchIsVendored bool
	var bestMatchVendorRoot string
	var matchError error

	for _, m := range matches {
		// Apply vendoring logic for Go libraries. A library in a vendor directory
		// is only visible in the parent tree. Vendored libraries supercede
		// non-vendored libraries, and libraries closer to from.Pkg supercede
		// those further up the tree.
		isVendored := false
		vendorRoot := ""
		parts := strings.Split(m.Label.Pkg, "/")
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] == "vendor" {
				isVendored = true
				vendorRoot = strings.Join(parts[:i], "/")
				break
			}
		}
		if isVendored {
		}
		if isVendored && !label.New(m.Label.Repo, vendorRoot, "").Contains(from) {
			// vendor directory not visible
			continue
		}
		if bestMatch.Label.Equal(label.NoLabel) || isVendored && (!bestMatchIsVendored || len(vendorRoot) > len(bestMatchVendorRoot)) {
			// Current match is better
			bestMatch = m
			bestMatchIsVendored = isVendored
			bestMatchVendorRoot = vendorRoot
			matchError = nil
		} else if (!isVendored && bestMatchIsVendored) || (isVendored && len(vendorRoot) < len(bestMatchVendorRoot)) {
			// Current match is worse
		} else {
			// Match is ambiguous
			// TODO: consider listing all the ambiguous rules here.
			matchError = fmt.Errorf("rule %s imports %q which matches multiple rules: %s and %s. # gazelle:resolve may be used to disambiguate", from, imp, bestMatch.Label, m.Label)
		}
	}
	if matchError != nil {
		return label.NoLabel, matchError
	}
	if bestMatch.Label.Equal(label.NoLabel) {
		return label.NoLabel, notFoundError
	}
	if bestMatch.IsSelfImport(from) {
		return label.NoLabel, skipImportError
	}
	return bestMatch.Label, nil
}

var modMajorRex = regexp.MustCompile(`/v\d+(?:/|$)`)

func resolveExternal(c *config.Config, rc *repo.RemoteCache, imp string) (label.Label, error) {
	// If we're in module mode, use "go list" to find the module path and
	// repository name. Otherwise, use special cases (for github.com, golang.org)
	// or send a GET with ?go-get=1 to find the root. If the path contains
	// a major version suffix (e.g., /v2), treat it as a module anyway though.
	//
	// Eventually module mode will be the only mode. But for now, it's expensive
	// and not the common case, especially when known repositories aren't
	// listed in WORKSPACE (which is currently the case within go_repository).
	gc := getGoConfig(c)
	moduleMode := gc.moduleMode
	if !moduleMode {
		moduleMode = pathWithoutSemver(imp) != ""
	}

	var prefix, repo string
	var err error
	if moduleMode {
		prefix, repo, err = rc.Mod(imp)
	} else {
		prefix, repo, err = rc.Root(imp)
	}
	if err != nil {
		return label.NoLabel, err
	}

	var pkg string
	if pathtools.HasPrefix(imp, prefix) {
		pkg = pathtools.TrimPrefix(imp, prefix)
	} else if impWithoutSemver := pathWithoutSemver(imp); pathtools.HasPrefix(impWithoutSemver, prefix) {
		// We may have used minimal module compatibility to resolve a path
		// without a semantic import version suffix to a repository that has one.
		pkg = pathtools.TrimPrefix(impWithoutSemver, prefix)
	}

	// Determine what naming convention is used by the repository.
	// If there is no known repository, it's probably declared in an http_archive
	// somewhere like go_rules_dependencies. Use the old naming convention.
	nc, ok := gc.repoNamingConvention[repo]
	if !ok {
		nc = goDefaultLibraryNamingConvention
	}
	if nc == importAliasNamingConvention {
		// If the repository is compatible with either naming convention, use
		// whichever is preferred in this directory to avoid churn.
		nc = gc.goNamingConvention
	}

	name := libNameByConvention(nc, imp, "")
	return label.New(repo, pkg, name), nil
}

func resolveVendored(gc *goConfig, imp string) (label.Label, error) {
	name := libNameByConvention(gc.goNamingConvention, imp, "")
	return label.New("", path.Join("vendor", imp), name), nil
}

func resolveProto(c *config.Config, ix *resolve.RuleIndex, rc *repo.RemoteCache, imp string, from label.Label) (label.Label, error) {
	if wellKnownProtos[imp] {
		return label.NoLabel, skipImportError
	}

	if l, ok := resolve.FindRuleWithOverride(c, resolve.ImportSpec{Lang: "proto", Imp: imp}, "go"); ok {
		return l, nil
	}

	if l, err := resolveWithIndexProto(c, ix, imp, from); err == nil || err == skipImportError {
		return l, err
	} else if err != notFoundError {
		return label.NoLabel, err
	}

	// As a fallback, guess the label based on the proto file name. We assume
	// all proto files in a directory belong to the same package, and the
	// package name matches the directory base name. We also assume that protos
	// in the vendor directory must refer to something else in vendor.
	rel := path.Dir(imp)
	if rel == "." {
		rel = ""
	}
	if from.Pkg == "vendor" || strings.HasPrefix(from.Pkg, "vendor/") {
		rel = path.Join("vendor", rel)
	}
	libName := libNameByConvention(getGoConfig(c).goNamingConvention, imp, "")
	return label.New("", rel, libName), nil
}

func resolveGoTool(c *config.Config, ix *resolve.RuleIndex, rc *repo.RemoteCache, imp string, from label.Label) (label.Label, error) {
	l, err := ResolveGo(c, ix, rc, imp, from)
	if err != nil {
		return l, err
	}
	if l.Name == "go_default_library" {
		l.Name = "go_tool_library"
	}
	return l, nil
}

// wellKnownProtos is the set of proto sets for which we don't need to add
// an explicit dependency in go_proto_library.
// TODO(jayconrod): generate from
// @io_bazel_rules_go//proto/wkt:WELL_KNOWN_TYPE_PACKAGES
var wellKnownProtos = map[string]bool{
	"google/protobuf/any.proto":             true,
	"google/protobuf/api.proto":             true,
	"google/protobuf/compiler/plugin.proto": true,
	"google/protobuf/descriptor.proto":      true,
	"google/protobuf/duration.proto":        true,
	"google/protobuf/empty.proto":           true,
	"google/protobuf/field_mask.proto":      true,
	"google/protobuf/source_context.proto":  true,
	"google/protobuf/struct.proto":          true,
	"google/protobuf/timestamp.proto":       true,
	"google/protobuf/type.proto":            true,
	"google/protobuf/wrappers.proto":        true,
}

func resolveWithIndexProto(c *config.Config, ix *resolve.RuleIndex, imp string, from label.Label) (label.Label, error) {
	matches := ix.FindRulesByImportWithConfig(c, resolve.ImportSpec{Lang: "proto", Imp: imp}, "go")
	if len(matches) == 0 {
		return label.NoLabel, notFoundError
	}
	if len(matches) > 1 {
		return label.NoLabel, fmt.Errorf("multiple rules (%s and %s) may be imported with %q from %s", matches[0].Label, matches[1].Label, imp, from)
	}
	if matches[0].IsSelfImport(from) {
		return label.NoLabel, skipImportError
	}
	return matches[0].Label, nil
}

func isGoLibrary(kind string) bool {
	return kind == "go_library" || isGoProtoLibrary(kind)
}

func isGoProtoLibrary(kind string) bool {
	return kind == "go_proto_library" || kind == "go_grpc_library"
}

// isExtraLibrary returns true if this rule is one of a handful of proto
// libraries generated by maybeGenerateExtraLib. It should not be indexed for
// dependency resolution.
func isExtraLibrary(r *rule.Rule) bool {
	if !strings.HasSuffix(r.Name(), "_gen") {
		return false
	}
	switch r.AttrString("importpath") {
	case "github.com/golang/protobuf/descriptor",
		"github.com/golang/protobuf/generator",
		"github.com/golang/protobuf/ptypes":
		return true
	default:
		return false
	}
}
