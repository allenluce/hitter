// +build test

package web

import (
	"net/http"

	"github.com/gernest/hot"
)

func ServeHome() http.HandlerFunc {
	var fs = http.FileServer(http.Dir("."))

	config := &hot.Config{
		Watch:          true,
		BaseName:       "templates",
		Dir:            "../assets",
		FilesExtension: []string{".html"},
		LeftDelim:      "XOX",
		RightDelim:     "OXO",
	}

	tpl, err := hot.New(config)
	if err != nil {
		panic(err)
	}

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
		tpl.Execute(w, "index.html", LoadData())
	}
}
