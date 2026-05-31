package store

import (
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"
	"k8s.io/klog/v2"
)

var (
	shadowBucket = []byte("shadow-claims")
)

// ShadowRecord persists the mapping from a composite claim to its shadow claims.
type ShadowRecord struct {
	CompositeClaimUID string        `json:"compositeClaimUID"`
	Namespace         string        `json:"namespace"`
	Shadows           []ShadowEntry `json:"shadows"`
}

type ShadowEntry struct {
	DriverName string `json:"driverName"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
}

// StateStore persists shadow claim mappings to BoltDB for crash recovery.
type StateStore struct {
	db *bolt.DB
}

func NewStateStore(path string) (*StateStore, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{})
	if err != nil {
		return nil, fmt.Errorf("open state db %s: %w", path, err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(shadowBucket)
		return err
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init buckets: %w", err)
	}

	return &StateStore{db: db}, nil
}

// SaveShadows persists shadow claim records for a composite claim UID.
func (s *StateStore) SaveShadows(record ShadowRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal shadow record: %w", err)
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(shadowBucket).Put([]byte(record.CompositeClaimUID), data)
	})
}

// GetShadows retrieves shadow claim records for a composite claim UID.
func (s *StateStore) GetShadows(compositeClaimUID string) (*ShadowRecord, error) {
	var record ShadowRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(shadowBucket).Get([]byte(compositeClaimUID))
		if data == nil {
			return nil
		}
		return json.Unmarshal(data, &record)
	})
	if err != nil {
		return nil, err
	}
	if record.CompositeClaimUID == "" {
		return nil, nil
	}
	return &record, nil
}

// DeleteShadows removes shadow claim records for a composite claim UID.
func (s *StateStore) DeleteShadows(compositeClaimUID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(shadowBucket).Delete([]byte(compositeClaimUID))
	})
}

// ListAll returns all persisted shadow records (for reconciliation on restart).
func (s *StateStore) ListAll() ([]ShadowRecord, error) {
	var records []ShadowRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(shadowBucket).ForEach(func(k, v []byte) error {
			var rec ShadowRecord
			if err := json.Unmarshal(v, &rec); err != nil {
				klog.Warningf("state: skip corrupt record %s: %v", string(k), err)
				return nil
			}
			records = append(records, rec)
			return nil
		})
	})
	return records, err
}

func (s *StateStore) Close() error {
	return s.db.Close()
}
