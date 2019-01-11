package thrift

import (
	"flag"
	"fmt"
	"log"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/rule"
)

type ThriftConfig struct {
	Mode Mode
}

func GetThriftConfig(c *config.Config) *ThriftConfig {
	tc := c.Exts[thriftName]
	if tc == nil {
		return nil
	}
	return tc.(*ThriftConfig)
}

type Mode int

const (
	DefaultMode Mode = iota
	DisableMode
)

type modeFlag struct {
	mode *Mode
}

func ModeFromString(s string) (Mode, error) {
	switch s {
	case "default":
		return DefaultMode, nil
	case "disable":
		return DisableMode, nil
	default:
		return 0, fmt.Errorf("unrecognized thrift mode: %q", s)
	}
}

func (m Mode) String() string {
	switch m {
	case DefaultMode:
		return "default"
	case DisableMode:
		return "disable"
	default:
		log.Panicf("unknown mode %d", m)
		return ""
	}
}

func (f *modeFlag) Set(value string) error {
	if mode, err := ModeFromString(value); err != nil {
		return err
	} else {
		*f.mode = mode
		return nil
	}
}

func (f *modeFlag) String() string {
	var mode Mode
	if f != nil && f.mode != nil {
		mode = *f.mode
	}
	return mode.String()
}

func (_ *thriftLang) RegisterFlags(fs *flag.FlagSet, cmd string, c *config.Config) {
	pc := &ThriftConfig{}
	c.Exts[thriftName] = pc

	fs.Var(&modeFlag{&pc.Mode}, "thrift", "default: generates a thrift_library rule per file\n\tdisable: does not touch thrift rules")
}

func (_ *thriftLang) CheckFlags(fs *flag.FlagSet, c *config.Config) error {
	return nil
}

func (_ *thriftLang) KnownDirectives() []string {
	return []string{"thrift"}
}

func (_ *thriftLang) Configure(c *config.Config, rel string, f *rule.File) {
	pc := &ThriftConfig{}
	*pc = *GetThriftConfig(c)
	c.Exts[thriftName] = pc
	if f != nil {
		for _, d := range f.Directives {
			switch d.Key {
			case "thrift":
				mode, err := ModeFromString(d.Value)
				if err != nil {
					log.Print(err)
					continue
				}
				pc.Mode = mode
			}
		}
	}
}
