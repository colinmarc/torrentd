package torrentd

import (
	"encoding/binary"
	"os"
	"path/filepath"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/anacrolix/torrent/types/infohash"
	"github.com/colinmarc/torrentd/log"
	bolt "go.etcd.io/bbolt"
)

const completionBucketKey = "cmpl"

func (td *Torrentd) storagePath(h infohash.T) string {
	return filepath.Join(td.conf.Client.DownloadPath, h.HexString())
}

func newStorage(path string, db *bolt.DB) (storage.ClientImpl, error) {
	err := os.MkdirAll(path, 0755)
	if err != nil {
		return nil, err
	}

	// TODO: we should make sure we have permissions, because the file storage doesn't.

	return storage.NewFileOpts(storage.NewFileClientOpts{
		ClientBaseDir: path,
		TorrentDirMaker: func(base string, info *metainfo.Info, hash metainfo.Hash) string {
			return filepath.Join(base, hash.HexString())
		},
		FilePathMaker: func(opts storage.FilePathMakerOpts) string {
			return filepath.Join(opts.File.Path...)
		},
		PieceCompletion: &storageCompletion{db}, // TODO
	}), nil
}

type storageCompletion struct {
	db *bolt.DB
}

func (c *storageCompletion) Get(pk metainfo.PieceKey) (cn storage.Completion, err error) {
	err = c.db.View(func(tx *bolt.Tx) error {
		cb := tx.Bucket([]byte(completionBucketKey))
		if cb == nil {
			return nil
		}

		b := cb.Bucket(pk.InfoHash[:])
		if b == nil {
			return nil
		}

		key := make([]byte, 4)
		binary.BigEndian.PutUint32(key, uint32(pk.Index))
		v := b.Get(key)
		if v == nil {
			return nil
		}

		switch v[0] {
		case 0x01:
			cn.Ok = true
			cn.Complete = true
		case 0x00:
			cn.Ok = true
			cn.Complete = false
		}
		return nil
	})

	return
}

func (c *storageCompletion) Set(pk metainfo.PieceKey, complete bool) error {
	return c.db.Update(func(tx *bolt.Tx) error {
		c, _ := tx.CreateBucketIfNotExists([]byte(completionBucketKey))
		b, _ := c.CreateBucketIfNotExists(pk.InfoHash[:])

		key := make([]byte, 4)
		binary.BigEndian.PutUint32(key, uint32(pk.Index))

		var v byte = 0x00
		if complete {
			v = 0x01
		}

		return b.Put(key, []byte{v})
	})
}

func (c *storageCompletion) Close() error {
	log.Debug("storage closed")
	return nil
}
