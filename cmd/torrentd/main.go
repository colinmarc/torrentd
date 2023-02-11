package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/colinmarc/torrentd"
	"github.com/pelletier/go-toml/v2"
	flag "github.com/spf13/pflag"
)

var (
	help             bool
	cfgFile          string
	web              string
	downloads, state string
)

func init() {
	flag.BoolVarP(&help, "help", "h", false, "print this message")
	flag.StringVarP(&cfgFile, "conf", "c", "", "path to config file")

	// These flags override whatever is in the config file.
	flag.StringVar(&web, "bind-web", "", "address to use for the web ui (defaults to localhost:9599)")
	flag.StringVar(&downloads, "downloads", "", "path to use for downloads (defaults to /var/lib/torrentd/downloads)")
	flag.StringVar(&state, "state", "", "path to use for internal state (defaults to /var/lib/torrentd)")
}

func main() {
	flag.Parse()
	if help {
		flag.Usage()
		os.Exit(0)
	}

	conf := torrentd.DefaultConfig()

	if cfgFile != "" {
		b, err := os.ReadFile(cfgFile)
		if err != nil {
			fatal(err)
		}

		log.Println("using config file:", cfgFile)
		err = toml.Unmarshal(b, conf)
		if err != nil {
			fatal("parsing config:", err)
		}
	}

	if web != "" {
		conf.Web.Bind = web
	}
	if downloads != "" {
		conf.Client.DownloadPath = downloads
	}
	if state != "" {
		conf.Client.StatePath = state
	}

	td, err := torrentd.NewFromConfig(conf)
	if err != nil {
		fatal(err)
	}

	// TODO remove
	conf.Debug.AnacrolixDebugLogging = true
	conf.Debug.BindPProf = "localhost:6060"

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer stop()

	err = td.Start(ctx)
	if err != nil {
		fatal(err)
	}
}

func fatal(vs ...any) {
	fmt.Fprintln(os.Stderr, vs...)
	os.Exit(1)
}
