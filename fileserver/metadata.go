package fileserver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileMeta holds metadata information for stored files.
type FileMeta struct {
	Key         string    `json:"key"`
	Filename    string    `json:"filename"`
	Size        int64     `json:"size"`
	ContentType string    `json:"contentType"`
	Owner       string    `json:"owner"`
	Checksum    string    `json:"checksum"`
	UploadTime  time.Time `json:"uploadTime"`
	NodeID      string    `json:"nodeId"`
	Replicas    int       `json:"replicas"`
}

// MetadataStore handles thread-safe persistent metadata storage per server node.
type MetadataStore struct {
	mu       sync.RWMutex
	filePath string
	entries  map[string]*FileMeta
}

// NewMetadataStore creates and initializes a metadata store for a given storage root.
func NewMetadataStore(storageRoot string) (*MetadataStore, error) {
	if err := os.MkdirAll(storageRoot, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage root directory: %w", err)
	}

	metaFile := filepath.Join(storageRoot, "metadata.json")
	ms := &MetadataStore{
		filePath: metaFile,
		entries:  make(map[string]*FileMeta),
	}

	if err := ms.load(); err != nil {
		return nil, err
	}

	return ms, nil
}

// load reads the metadata.json file if it exists.
func (ms *MetadataStore) load() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	data, err := os.ReadFile(ms.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read metadata file: %w", err)
	}

	if len(data) == 0 {
		return nil
	}

	var list []*FileMeta
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("failed to parse metadata JSON: %w", err)
	}

	for _, item := range list {
		ms.entries[item.Key] = item
	}

	return nil
}

// save writes all entries to metadata.json atomically.
func (ms *MetadataStore) saveLocked() error {
	list := make([]*FileMeta, 0, len(ms.entries))
	for _, v := range ms.entries {
		list = append(list, v)
	}

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	tmpFile := ms.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write tmp metadata file: %w", err)
	}

	return os.Rename(tmpFile, ms.filePath)
}

// Put adds or updates a file metadata record.
func (ms *MetadataStore) Put(meta *FileMeta) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.entries[meta.Key] = meta
	return ms.saveLocked()
}

// Get retrieves metadata for a key.
func (ms *MetadataStore) Get(key string) (*FileMeta, bool) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	meta, exists := ms.entries[key]
	if !exists {
		return nil, false
	}
	return meta, true
}

// Delete removes a file metadata record by key.
func (ms *MetadataStore) Delete(key string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	delete(ms.entries, key)
	return ms.saveLocked()
}

// List returns a copy of all metadata entries.
func (ms *MetadataStore) List() []*FileMeta {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	list := make([]*FileMeta, 0, len(ms.entries))
	for _, v := range ms.entries {
		list = append(list, v)
	}
	return list
}
