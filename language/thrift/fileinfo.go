package thrift

import (
	"bytes"
	"io/ioutil"
	"log"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// FileInfo contains metadata extracted from a .thrift file.
type FileInfo struct {
	Path, Name, RelPath string

	Includes []string
	Services []string
}

var thriftRe = buildThriftRegexp()

func thriftFileInfo(dir, rel, name string) FileInfo {
	info := FileInfo{
		Path:    filepath.Join(dir, name),
		RelPath: filepath.Join(rel, name),
		Name:    name,
	}
	content, err := ioutil.ReadFile(info.Path)
	if err != nil {
		log.Printf("%s: error reading thrift file: %v", info.Path, err)
		return info
	}

	for _, match := range thriftRe.FindAllSubmatch(content, -1) {
		switch {
		case match[includeSubexpIndex] != nil:
			imp := unquoteThriftString(match[includeSubexpIndex])
			cleanedPath := filepath.Clean(filepath.Join(rel, imp))
			info.Includes = append(info.Includes, cleanedPath)
		case match[servicesSubexpIndex] != nil:
			info.Services = append(info.Services, string(match[servicesSubexpIndex]))

		default:
			// Comment matched. Nothing to extract.
		}
	}
	sort.Strings(info.Includes)
	sort.Strings(info.Services)

	return info
}

const (
	includeSubexpIndex  = 1
	servicesSubexpIndex = 2
)

// Mostly stolen from proto, should replace with the thriftrw go library to actually parse files
func buildThriftRegexp() *regexp.Regexp {
	hexEscape := `\\[xX][0-9a-fA-f]{2}`
	octEscape := `\\[0-7]{3}`
	charEscape := `\\[abfnrtv'"\\]`
	charValue := strings.Join([]string{hexEscape, octEscape, charEscape, "[^\x00\\'\\\"\\\\]"}, "|")
	strLit := `'(?:` + charValue + `|")*'|"(?:` + charValue + `|')*"`
	importStmt := `\binclude\s*(?P<import>` + strLit + `)\s+`
	comment := `//[^\n]*`
	service := `\bservice\s*(?P<service>\w+)[\s]*{`
	thriftReSrc := strings.Join([]string{importStmt, service, comment}, "|")
	return regexp.MustCompile(thriftReSrc)
}

func unquoteThriftString(q []byte) string {
	// Adjust quotes so that Unquote is happy. We need a double quoted string
	// without unescaped double quote characters inside.
	noQuotes := bytes.Split(q[1:len(q)-1], []byte{'"'})
	if len(noQuotes) != 1 {
		for i := 0; i < len(noQuotes)-1; i++ {
			if len(noQuotes[i]) == 0 || noQuotes[i][len(noQuotes[i])-1] != '\\' {
				noQuotes[i] = append(noQuotes[i], '\\')
			}
		}
		q = append([]byte{'"'}, bytes.Join(noQuotes, []byte{'"'})...)
		q = append(q, '"')
	}
	if q[0] == '\'' {
		q[0] = '"'
		q[len(q)-1] = '"'
	}

	s, err := strconv.Unquote(string(q))
	if err != nil {
		log.Panicf("unquoting string literal %s from proto: %v", q, err)
	}
	return s
}
