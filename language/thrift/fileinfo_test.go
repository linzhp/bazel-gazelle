package thrift

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestProtoRegexpGroupNames(t *testing.T) {
	names := thriftRe.SubexpNames()
	nameMap := map[string]int{
		"include": includeSubexpIndex,
	}
	for name, index := range nameMap {
		if names[index] != name {
			t.Errorf("include regexp subexp %d is %s ; want %s", index, names[index], name)
		}
	}
	if len(names)-1 != len(nameMap) {
		t.Errorf("include regexp has %d groups ; want %d", len(names), len(nameMap))
	}
}

func TestProtoFileInfo(t *testing.T) {
	for _, tc := range []struct {
		desc, name, thrift string
		want               FileInfo
	}{
		{
			desc:   "empty",
			name:   "empty^file.thrift",
			thrift: "",
			want:   FileInfo{},
		}, {
			desc: "include simple",
			name: "imp.thrift",
			thrift: `include 'single.thrift';
		include "double.thrift";`,
			want: FileInfo{
				Includes: []string{"double.thrift", "single.thrift"},
			},
		}, {
			desc: "include quote",
			name: "quote.thrift",
			thrift: `include '""\".thrift"';
		include "'.thrift";`,
			want: FileInfo{
				Includes: []string{"\"\"\".thrift\"", "'.thrift"},
			},
		}, {
			desc:   "include escape",
			name:   "escape.thrift",
			thrift: `include '\n\012\x0a.thrift';`,
			want: FileInfo{
				Includes: []string{"\n\n\n.thrift"},
			},
		}, {
			desc: "include two",
			name: "two.thrift",
			thrift: `include "./first.thrift";
		include "second.thrift";`,
			want: FileInfo{
				Includes: []string{"first.thrift", "second.thrift"},
			},
		},
		{
			desc: "include service",
			name: "folder/two.thrift",
			thrift: `include "../first.thrift";
service testing
{`,
			want: FileInfo{
				Includes: []string{"first.thrift"},
				Services: []string{"testing"},
			},
		},
		{
			desc: "include two folders",
			name: "folder/two.thrift",
			thrift: `include "../first.thrift";
include "second.thrift";`,
			want: FileInfo{
				Includes: []string{"first.thrift", "folder/second.thrift"},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			root, err := ioutil.TempDir(os.Getenv("TEST_TEMPDIR"), "TestProtoFileinfo")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			p := filepath.Join(root, tc.name)
			if err := os.MkdirAll(filepath.Dir(p), os.ModePerm); err != nil {
				t.Fatal(err)
			}
			if err := ioutil.WriteFile(p, []byte(tc.thrift), 0600); err != nil {
				t.Fatal(err)
			}

			got := thriftFileInfo(filepath.Dir(p), filepath.Dir(tc.name), filepath.Base(tc.name))

			// Clear fields we don't care about for testing.
			got = FileInfo{
				Includes: got.Includes,
				Services: got.Services,
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %#v; want %#v", got, tc.want)
			}
		})
	}
}
