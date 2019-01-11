package thrift

import (
	"errors"
	"fmt"
	"log"
	"path"
	"sort"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/label"
	"github.com/bazelbuild/bazel-gazelle/repo"
	"github.com/bazelbuild/bazel-gazelle/resolve"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

func (_ *thriftLang) Imports(c *config.Config, r *rule.Rule, f *rule.File) []resolve.ImportSpec {
	rel := f.Pkg
	src := r.AttrString("src")
	return []resolve.ImportSpec{{Lang: "thrift", Imp: path.Join(rel, src)}}
}

func (_ *thriftLang) Embeds(r *rule.Rule, from label.Label) []label.Label {
	return nil
}

func (_ *thriftLang) Resolve(c *config.Config, ix *resolve.RuleIndex, rc *repo.RemoteCache, r *rule.Rule, importsRaw interface{}, from label.Label) {
	imports := importsRaw.([]string)
	r.DelAttr("deps")
	depSet := make(map[string]bool)
	for _, imp := range imports {
		l, err := resolveThrift(c, ix, r, imp, from)
		if err == skipImportError {
			continue
		} else if err != nil {
			log.Print(err)
		} else {
			l = l.Rel(from.Repo, from.Pkg)
			depSet[l.String()] = true
		}
	}
	if len(depSet) > 0 {
		deps := make([]string, 0, len(depSet))
		for dep := range depSet {
			deps = append(deps, dep)
		}
		sort.Strings(deps)
		r.SetAttr("deps", deps)
	}
}

var (
	skipImportError = errors.New("std import")
	notFoundError   = errors.New("not found")
)

func resolveThrift(c *config.Config, ix *resolve.RuleIndex, r *rule.Rule, imp string, from label.Label) (label.Label, error) {
	if !strings.HasSuffix(imp, ".thrift") {
		return label.NoLabel, fmt.Errorf("can't import non-thrift: %q", imp)
	}

	if l, ok := resolve.FindRuleWithOverride(c, resolve.ImportSpec{Imp: imp, Lang: "thrift"}, "thrift"); ok {
		return l, nil
	}

	if l, err := resolveWithIndex(ix, imp, from); err == nil || err == skipImportError {
		return l, err
	} else if err != notFoundError {
		return label.NoLabel, err
	}

	rel := path.Dir(imp)
	if rel == "." {
		rel = ""
	}
	name := RuleName(rel)
	return label.New("", rel, name), nil
}

func resolveWithIndex(ix *resolve.RuleIndex, imp string, from label.Label) (label.Label, error) {
	matches := ix.FindRulesByImport(resolve.ImportSpec{Lang: "thrift", Imp: imp}, "thrift")
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
