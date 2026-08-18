package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

//go:embed templates static
var content embed.FS

// funcs are shared by every template set.
var funcs = template.FuncMap{
	"stars":   stars,
	"timefmt": timefmt,
}

// pageNames are the standalone pages, each parsed into its own template set so
// their "content" block definitions don't collide with each other.
var pageNames = []string{"login", "register", "dashboard", "books"}

// fragments holds base.html plus all partials, shared across pages.
var fragments *template.Template

// pages maps a page name to a template set containing base + partials + page.
var pages map[string]*template.Template

func init() {
	var err error
	fragments, err = template.New("").Funcs(funcs).ParseFS(content,
		"templates/*.html",
		"templates/partials/*.html",
	)
	if err != nil {
		panic(fmt.Sprintf("parse base/partials templates: %v", err))
	}

	pages = make(map[string]*template.Template, len(pageNames))
	for _, name := range pageNames {
		// Clone the shared set, then parse this page's file into the clone.
		set, err := fragments.Clone()
		if err != nil {
			panic(fmt.Sprintf("clone template set: %v", err))
		}
		body, err := content.ReadFile("templates/pages/" + name + ".html")
		if err != nil {
			panic(fmt.Sprintf("read page %s: %v", name, err))
		}
		if _, err := set.New(name).Parse(string(body)); err != nil {
			panic(fmt.Sprintf("parse page %s: %v", name, err))
		}
		pages[name] = set
	}
}

// templateLog is set by NewServer so render failures are visible in logs.
var templateLog = slog.Default()

// render executes a named template with data. Page names resolve to their own
// template set; anything else is treated as a fragment from the shared set.
func render(w http.ResponseWriter, r *http.Request, name string, data any) {
	if set, ok := pages[name]; ok {
		if err := set.ExecuteTemplate(w, name, data); err != nil {
			templateLog.Error("template execution failed", "template", name, "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
		return
	}
	if err := fragments.ExecuteTemplate(w, name, data); err != nil {
		templateLog.Error("template execution failed", "template", name, "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

// staticFiles serves the embedded static assets with long-lived caching.
func staticFiles() http.Handler {
	sub, err := fs.Sub(content, "static")
	if err != nil {
		panic(fmt.Sprintf("embed static: %v", err))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".js") || strings.HasSuffix(r.URL.Path, ".css") {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		http.FileServer(http.FS(sub)).ServeHTTP(w, r)
	})
}

// stars renders a 1-5 star rating as plain text (safe, no escaping needed).
func stars(n int) string {
	if n < 0 {
		n = 0
	}
	if n > 5 {
		n = 5
	}
	return strings.Repeat("★", n) + strings.Repeat("☆", 5-n)
}

func timefmt(t time.Time) string {
	return t.Format("Jan 2, 2006")
}
