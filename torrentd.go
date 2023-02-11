// package torrentd provides a lightweight, headless torrent client.
package torrentd

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	anacrolixlog "github.com/anacrolix/log"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/types/infohash"
	"github.com/colinmarc/torrentd/log"
	"github.com/r3labs/sse/v2"
	bolt "go.etcd.io/bbolt"

	_ "net/http/pprof"
)

type Torrentd struct {
	ctx    context.Context
	conf   *Config
	client *torrent.Client
	web    *http.Server
	events *sse.Server
	mux    *http.ServeMux
	db     *bolt.DB

	torrents     map[infohash.T]*torrentState
	torrentsLock sync.Mutex
}

// TODO dump torrent files somwhere too?

// NewFromConfig constructs a torrentd instance based on the config.
func NewFromConfig(conf *Config) (*Torrentd, error) {
	err := os.MkdirAll(conf.Client.StatePath, 0755)
	if err != nil {
		return nil, err
	}

	log.Infof("using state path: %s", conf.Client.StatePath)
	db, err := bolt.Open(filepath.Join(conf.Client.StatePath, "torrentd.db"), 0644, &bolt.Options{
		Timeout: time.Second,
	})
	if err != nil {
		return nil, err
	}

	clientConfig := torrent.NewDefaultClientConfig()

	host, p, err := net.SplitHostPort(conf.Client.Bind)
	if err != nil {
		return nil, fmt.Errorf("invalid client.bind addr: %w", err)
	}

	port, err := strconv.Atoi(p)
	if err != nil {
		return nil, fmt.Errorf("invalid client.bind addr: %w", err)
	}

	clientConfig.ListenHost = func(network string) string { return host }
	clientConfig.ListenPort = port
	clientConfig.NoDefaultPortForwarding = !conf.Client.EnableUpnp
	clientConfig.Seed = true
	clientConfig.DisableWebseeds = true

	if conf.Debug.AnacrolixDebugLogging {
		clientConfig.Logger = anacrolixlog.Default.FilterLevel(anacrolixlog.Debug)
	}

	log.Infof("using storage path: %s", conf.Client.DownloadPath)
	storage, err := newStorage(conf.Client.DownloadPath, db)
	if err != nil {
		return nil, fmt.Errorf("initializing download path: %w", err)
	}
	clientConfig.DefaultStorage = storage

	client, err := torrent.NewClient(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("error initializing client: %w", err)
	}

	// TODO speed limits

	td := Torrentd{
		conf:     conf,
		client:   client,
		db:       db,
		mux:      http.NewServeMux(),
		events:   sse.New(),
		torrents: make(map[infohash.T]*torrentState),
	}

	web := http.Server{
		Addr:    conf.Web.Bind,
		Handler: td.mux,
		// TODO timeouts
	}

	if conf.Web.TLSCertFile != "" && conf.Web.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(conf.Web.TLSCertFile, conf.Web.TLSKeyFile)
		if err != nil {
			return nil, err
		}

		web.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	}

	td.initHandlers()
	td.web = &web
	return &td, nil
}

// Start starts the web UI and torrent client. It blocks until ctx is canceled.
func (td *Torrentd) Start(ctx context.Context) error {
	td.ctx = ctx

	// Load existing torrent state.
	err := td.loadAll()
	if err != nil {
		log.Error(fmt.Errorf("loading state: %w", err))
	}

	// TODO load from torrent_path

	go func() {
		if td.web.TLSConfig != nil {
			log.Infof("listening on https://%s", td.web.Addr)
			td.web.ListenAndServeTLS("", "")
		} else {
			log.Infof("listening on http://%s", td.web.Addr)
			td.web.ListenAndServe()
		}
	}()

	if td.conf.Debug.BindPProf != "" {
		log.Infof("starting pprof server at %s", td.conf.Debug.BindPProf)
		go http.ListenAndServe(td.conf.Debug.BindPProf, nil)
	}

	<-ctx.Done()

	log.Info("shutting down...")

	td.events.Close()
	td.web.Shutdown(context.Background())

	// Wait for all watchers to exit. They should also respect the
	// context's cancellation signal.
	td.torrentsLock.Lock()
	defer td.torrentsLock.Unlock()
	for _, st := range td.torrents {
		<-st.done
	}

	td.client.Close()
	<-td.client.Closed()

	td.db.Close()
	return nil
}
