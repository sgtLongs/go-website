// Package persistence provides the durable key/value storage shared by the
// lobby and realtime services. Domain packages remain responsible for their
// own JSON schemas so the storage layer never needs to understand game data.
package persistence

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	LobbiesBucket = []byte("lobbies")
	GrantsBucket  = []byte("grants")
	RoomsBucket   = []byte("rooms")
)

// Store wraps the one bbolt database used by the server.
type Store struct {
	db *bolt.DB
}

type Entry struct {
	Bucket []byte
	Key    []byte
	Value  []byte
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("persistence path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create persistence directory: %w", err)
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open persistence database: %w", err)
	}
	store := &Store{db: db}
	if err := store.db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range [][]byte{LobbiesBucket, GrantsBucket, RoomsBucket} {
			if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize persistence database: %w", err)
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Put(bucket, key, value []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucket).Put(key, value)
	})
}

func (s *Store) PutAll(entries ...Entry) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, entry := range entries {
			if err := tx.Bucket(entry.Bucket).Put(entry.Key, entry.Value); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) Get(bucket, key []byte) ([]byte, bool, error) {
	var result []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(bucket).Get(key)
		if value != nil {
			result = append([]byte(nil), value...)
		}
		return nil
	})
	return result, result != nil, err
}

func (s *Store) Delete(bucket, key []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucket).Delete(key)
	})
}

func (s *Store) ForEach(bucket []byte, visit func(key, value []byte) error) error {
	return s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucket).ForEach(func(key, value []byte) error {
			return visit(append([]byte(nil), key...), append([]byte(nil), value...))
		})
	})
}

// DeleteLobby removes a lobby, its realtime snapshot, and all of its access
// grants in one durable transaction.
func (s *Store) DeleteLobby(id string, grantKeys [][]byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(LobbiesBucket).Delete([]byte(id)); err != nil {
			return err
		}
		if err := tx.Bucket(RoomsBucket).Delete([]byte(id)); err != nil {
			return err
		}
		grants := tx.Bucket(GrantsBucket)
		for _, key := range grantKeys {
			if err := grants.Delete(key); err != nil {
				return err
			}
		}
		return nil
	})
}
