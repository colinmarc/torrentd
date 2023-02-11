package log

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/rs/zerolog"
)

var zl = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).With().Timestamp().Logger()

func Debug(msg string) {
	zl.Debug().Msg(msg)
}

func Debugf(msg string, args ...any) {
	Debug(fmt.Sprintf(msg, args...))
}

func Info(msg string) {
	zl.Info().Msg(msg)
}

func Infof(msg string, args ...any) {
	Info(fmt.Sprintf(msg, args...))
}

func Error(err error) {
	zl.Error().Msg(err.Error())
}

func Request(r *http.Request, status int, latency time.Duration) {
	zl.Info().Dur("latency", latency).Int("status", status).Msgf("%s %s", r.Method, r.URL.Path)
}
