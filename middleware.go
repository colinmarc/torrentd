package torrentd

import (
	"net/http"
	"time"

	"github.com/colinmarc/torrentd/log"
)

type statusRecorder struct {
	status int
	http.ResponseWriter
}

func (sr *statusRecorder) WriteHeader(status int) {
	sr.status = status
	sr.ResponseWriter.WriteHeader(status)
}

func logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		st := time.Now()
		next.ServeHTTP(rec, r)

		status := rec.status
		if status == 0 {
			status = 200
		}

		log.Request(r, status, time.Since(st))
	})
}
