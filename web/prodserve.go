// +build !test

package web

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"strings"
)

type EmbeddedFile struct {
	name string
	io.Closer
	io.Reader
	io.Seeker
}

func (ef EmbeddedFile) Readdir(count int) (fi []os.FileInfo, err error) {
	return
}

func (ef EmbeddedFile) Stat() (fi os.FileInfo, err error) {
	return AssetInfo(ef.name)
}

func (ef EmbeddedFile) Close() error {
	return nil
}

type EmbeddedFileSystem struct{}

func (e EmbeddedFileSystem) Open(name string) (http.File, error) {
	if strings.HasPrefix(name, "/") {
		name = name[1:]
	}
	data, err := Asset(name)
	if err != nil {
		fmt.Printf("Got an %s\n", err)
		return nil, err
	}
	file := EmbeddedFile{Reader: bytes.NewReader(data), name: name}
	return file, nil
}

func ServeHome() http.HandlerFunc {
	var fs = http.FileServer(EmbeddedFileSystem{})

	index, err := Asset("assets/index.html")
	if err != nil {
		panic(err)
	}

	tpl := template.Must(template.New("index.html").Delims("XOX", "OXO").Parse(string(index)))

	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			fs.ServeHTTP(w, r)
			return
		}
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tpl.Execute(w, LoadData())
	}
}
