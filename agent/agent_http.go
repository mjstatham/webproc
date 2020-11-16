package agent

import (
	"net/http"
	"path/filepath"
)

func (a *agent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.root.ServeHTTP(w, r)
}

func (a *agent) router(w http.ResponseWriter, r *http.Request) {
	switch filepath.Base(r.URL.Path) {
	case "healthz":
		a.serveHealth(w, r)
	default:
		a.serveDefault(w, r)
	}
}

func (a *agent) serveDefault(w http.ResponseWriter, r *http.Request) {
	if a.data.Running {
		w.WriteHeader(404)
	} else {
		w.WriteHeader(502)
	}
}

func (a *agent) serveHealth(w http.ResponseWriter, r *http.Request) {
	if a.data.Running {
		w.WriteHeader(200)
	} else {
		w.WriteHeader(502)
	}
}
