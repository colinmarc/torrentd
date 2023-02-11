package torrentd

import (
	"fmt"
	"io"
	"net/http"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/types/infohash"
	"github.com/colinmarc/torrentd/log"
	"github.com/colinmarc/torrentd/pb"
	"github.com/julienschmidt/httprouter"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func (td *Torrentd) initHandlers() {
	// API methods.
	router := httprouter.New()
	router.Handler("GET", "/events", td.events)
	router.HandlerFunc("POST", "/torrents", td.postTorrent)
	router.HandlerFunc("GET", "/torrents", td.getTorrents)
	router.HandlerFunc("GET", "/torrents/:h", td.getTorrent)
	router.HandlerFunc("DELETE", "/torrents/:h", td.deleteTorrent)
	router.HandlerFunc("GET", "/torrents/:h/stream", td.getTorrentStream)

	td.mux.Handle("/", logger(router))
	// mux.Handle("/static", static)
}

func (td *Torrentd) postTorrent(w http.ResponseWriter, r *http.Request) {
	var spec *torrent.TorrentSpec
	var err error

	switch r.Header.Get("content-type") {
	case "application/x-bittorrent":
		mi, parseErr := metainfo.Load(r.Body)
		if parseErr != nil {
			err = parseErr
			break
		}

		spec, err = torrent.TorrentSpecFromMetaInfoErr(mi)
	case "text/x-uri":
		b, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		spec, err = torrent.TorrentSpecFromMagnetUri(string(b))
	}

	if err != nil {
		log.Infof("invalid torrent: %v", err)
		w.WriteHeader(400)
		return
	}

	t, err := td.add(spec)
	if err != nil {
		w.WriteHeader(500)
		return
	}

	writeResp(w, t)
}

func (td *Torrentd) getTorrents(w http.ResponseWriter, r *http.Request) {
	resp := pb.TorrentList{
		Torrents: td.list(),
	}

	writeResp(w, &resp)
}

func (td *Torrentd) getTorrent(w http.ResponseWriter, r *http.Request) {

}

func (td *Torrentd) deleteTorrent(w http.ResponseWriter, r *http.Request) {
	var h infohash.T
	err := h.FromHexString(httprouter.ParamsFromContext(r.Context()).ByName("h"))
	if err != nil {
		log.Error(err)
		w.WriteHeader(404)
		return
	}

	if !td.remove(h) {
		w.WriteHeader(404)
	}
}

func (td *Torrentd) getTorrentStream(w http.ResponseWriter, r *http.Request) {

}

func writeResp(w http.ResponseWriter, msg proto.Message) {
	b, err := protojson.Marshal(msg)
	if err != nil {
		log.Error(fmt.Errorf("error marshaling response: %w", err))
	}

	w.Write(b)
}
