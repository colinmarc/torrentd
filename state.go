package torrentd

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"sync/atomic"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/types/infohash"
	"github.com/colinmarc/torrentd/log"
	"github.com/colinmarc/torrentd/pb"
	"github.com/r3labs/sse/v2"
	bolt "go.etcd.io/bbolt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	stateBucketName    = "t"
	metainfoBucketName = "mi"
)

type torrentState struct {
	infohash infohash.T
	ptr      atomic.Pointer[pb.Torrent]
	lastErr  atomic.Pointer[error]
	done     chan struct{}
}

func (td *Torrentd) loadAll() error {
	torrents := make(map[infohash.T][]byte)
	metainfos := make(map[infohash.T][]byte)
	st := time.Now()

	td.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(stateBucketName))
		if b == nil {
			return nil
		}

		mib := tx.Bucket([]byte(metainfoBucketName))
		if mib == nil {
			return nil
		}

		return b.ForEach(func(k, v []byte) error {
			h := (*infohash.T)(k)

			torrents[*h] = v
			metainfos[*h] = mib.Get(k)
			return nil
		})
	})

	log.Infof("loading %d torrents", len(torrents))
	td.torrentsLock.Lock()
	defer td.torrentsLock.Unlock()

	for h, b := range torrents {
		var v pb.Torrent
		err := proto.Unmarshal(b, &v)
		if err != nil {
			log.Error(fmt.Errorf("loading state for %s: %w", h.HexString(), err))
			continue
		}

		mi, err := metainfo.Load(bytes.NewReader(metainfos[h]))
		if err != nil {
			log.Error(fmt.Errorf("loading metainfo for %s: %w", h.HexString(), err))
			continue
		}

		spec := torrent.TorrentSpecFromMetaInfo(mi)
		t, _, err := td.client.AddTorrentSpec(spec)
		if err != nil {
			log.Error(fmt.Errorf("adding existing torrent %s: %w", v.HashHex, err))
			continue
		}

		log.Debugf("added to underlying client: %s", h.HexString())

		td.torrents[t.InfoHash()] = td.watch(t, &v)
		go func() {
			<-t.GotInfo()
			t.DownloadAll()
		}()
	}

	log.Infof("done loading torrents (took %v)", time.Since(st))
	return nil
}

// add adds a torrent to the underlying client, and then starts to
// track it.
func (td *Torrentd) add(spec *torrent.TorrentSpec) (*pb.Torrent, error) {
	h := spec.InfoHash

	// This automatically merges, and also checks for existing piece
	// completion data (but not the files themselves).
	t, _, err := td.client.AddTorrentSpec(spec)
	if err != nil {
		return nil, err
	}

	log.Debugf("added to underlying client: %s", t.InfoHash().HexString())

	td.torrentsLock.Lock()
	defer td.torrentsLock.Unlock()

	if st, ok := td.torrents[h]; ok {
		return st.ptr.Load(), nil
	}

	b := new(bytes.Buffer)
	err = t.Metainfo().Write(b)
	if err != nil {
		log.Error(fmt.Errorf("error marshaling metainfo: %w", err))
	}

	v := &pb.Torrent{
		Hash:    h[:],
		HashHex: h.HexString(),
		Name:    spec.DisplayName,
		State:   pb.TorrentState_INITIALIZING,
		Started: timestamppb.New(time.Now()),
	}

	err = td.save(v, b.Bytes())
	if err != nil {
		log.Error(fmt.Errorf("error saving torrent state: %w", err))
	}

	td.torrents[h] = td.watch(t, v)
	go func() {
		<-t.GotInfo()
		t.DownloadAll()
	}()

	return v, nil
}

// remove removes a torrent from the state and deletes any data. It returns
// true if the torrent was found.
func (td *Torrentd) remove(h infohash.T) bool {
	log.Infof("removing torrent: %s", h.HexString())

	td.torrentsLock.Lock()
	defer td.torrentsLock.Unlock()

	watcher := td.torrents[h]
	delete(td.torrents, h)

	if watcher != nil {
		go func() {
			// The watcher should finish once the state disappears from the
			// underlying client.
			<-watcher.done
			err := td.cleanup(h)
			if err != nil {
				log.Error(fmt.Errorf("error during torrent cleanup: %w", err))
			}
		}()

		t, _ := td.client.Torrent(h)
		if t != nil {
			go t.Drop()
		}

		return true
	} else {
		return false
	}
}

// save writes the torrent to disk, using protobuf serialization. It also saves
// the metainfo bytes in the same transaction, if provided.
func (td *Torrentd) save(v *pb.Torrent, mi []byte) error {
	b, err := proto.Marshal(v)
	if err != nil {
		return fmt.Errorf("error marshaling torrent state: %w", err)
	}

	err = td.db.Update(func(tx *bolt.Tx) error {
		stb, _ := tx.CreateBucketIfNotExists([]byte(stateBucketName))
		err := stb.Put(v.Hash, b)
		if err != nil {
			return err
		}

		if mi != nil {
			mib, _ := tx.CreateBucketIfNotExists([]byte(metainfoBucketName))
			err := mib.Put(v.Hash, mi)

			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	log.Debugf("wrote torrent state: %s", v.HashHex)

	jsonb, err := protojson.Marshal(v)
	if err != nil {
		return fmt.Errorf("error marshaling torrent state: %w", err)

	}
	td.events.Publish("torrents", &sse.Event{
		Data: jsonb,
	})

	return nil
}

func (td *Torrentd) cleanup(h infohash.T) error {
	hex := h.HexString()

	td.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(stateBucketName))
		if bucket != nil {
			err := bucket.Delete(h[:])
			if err != nil {
				return err
			}
		}

		bucket.Delete(h[:])

		compBucket := tx.Bucket([]byte(completionBucketKey))
		if compBucket != nil {
			return tx.DeleteBucket(h[:])
		}

		return nil
	})

	// Publish a tombstone to subscribers.
	b, err := proto.Marshal(&pb.Torrent{
		Hash:    h[:],
		HashHex: hex,
		State:   pb.TorrentState_DELETED,
	})
	if err != nil {
		return fmt.Errorf("marshaling tombstone: %w", err)
	}

	td.events.Publish("torrents", &sse.Event{
		Data: b,
	})

	return os.RemoveAll(td.storagePath(h))
}

func (td *Torrentd) list() []*pb.Torrent {
	td.torrentsLock.Lock()
	defer td.torrentsLock.Unlock()

	var res []*pb.Torrent
	for _, state := range td.torrents {
		res = append(res, state.ptr.Load())
	}

	return res
}

func (td *Torrentd) watch(t *torrent.Torrent, v *pb.Torrent) *torrentState {
	h := t.InfoHash()
	hex := h.HexString()

	state := &torrentState{
		infohash: h,
		done:     make(chan struct{}),
	}

	// Store the initial (or existing) value.
	state.ptr.Store(v)

	// Store the last error
	t.SetOnWriteChunkError(func(err error) {
		log.Error(fmt.Errorf("torrent error: %s: %v", hex, err))
		state.lastErr.Store(&err)
	})

	// Periodically poll the client for information about the torrent. We want
	// to poll active torrents very frequently and inactive torrents
	// infrequently.
	go func() {
		defer close(state.done)
		var sleep time.Duration

	Polling:
		for {
			if sleep > 0 {
				select {
				case <-time.NewTimer(sleep).C:
				case <-td.ctx.Done():
					break Polling
				}
			}

			t, ok := td.client.Torrent(h)
			if t == nil || !ok {
				// The torrent disappeared, or was removed.
				break Polling
			}

			select {
			case <-t.GotInfo():
			case <-td.ctx.Done():
				break Polling
			}

			info := t.Info()
			newv := &pb.Torrent{
				Hash:           h[:],
				HashHex:        hex,
				Name:           info.Name,
				Started:        v.Started,
				TotalSize:      t.Length(),
				TotalCompleted: t.BytesCompleted(),
				// TODO stats
				TotalUpload:    0,
				TotalDownload:  0,
				UploadBps:      0,
				DownloadBps:    0,
				ConnectedPeers: 0,
				Files:          []*pb.Torrent_File{},
			}

			for _, f := range t.Files() {
				newv.Files = append(newv.Files, &pb.Torrent_File{
					Path:      f.DisplayPath(),
					Size:      f.Length(),
					Completed: f.BytesCompleted(),
				})
			}

			if newv.TotalCompleted < newv.TotalSize {
				if v.State != pb.TorrentState_DOWNLOADING {
					log.Debugf("started downloading %s", newv.HashHex)
				}

				newv.State = pb.TorrentState_DOWNLOADING
				log.Debugf("progress: %s: %d%%", newv.HashHex, (100*newv.TotalCompleted)/newv.TotalSize)
			} else {
				if v.State != pb.TorrentState_SEEDING {
					log.Debugf("seeding %s", newv.HashHex)
				}

				newv.State = pb.TorrentState_SEEDING
			}

			err := td.save(newv, nil)
			if err != nil {
				log.Error(fmt.Errorf("error saving torrent state: %w", err))
			}

			v = newv
			state.ptr.Store(v)

			// TODO variable sleep
			sleep = jitter(100 * time.Millisecond)
		}
	}()

	return state
}

func jitter(d time.Duration) time.Duration {
	r := rand.Float64() * float64(d)
	r = 0.5*r + 0.5*float64(d)
	return time.Duration(0.5*r + 0.5*float64(d))
}
