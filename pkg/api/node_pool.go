package api

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync/atomic"

	"github.com/yigithankarabulut/distributed-file-storage/fileserver"
)

var (
	ErrNoHealthyNodes = errors.New("no healthy storage nodes available in cluster")
)

// GetHealthyServers returns all currently responsive, running storage servers.
func (a *App) GetHealthyServers() []*fileserver.FileServer {
	var healthy []*fileserver.FileServer
	for _, srv := range a.Servers {
		if srv != nil && srv.IsHealthy() {
			healthy = append(healthy, srv)
		}
	}
	return healthy
}

// SelectServer picks an active storage server using consistent hashing on the key.
// Consistent hashing ensures read/write affinity to primary replica nodes while
// dynamically adapting when nodes join or fail.
func (a *App) SelectServer(key string) (*fileserver.FileServer, error) {
	healthy := a.GetHealthyServers()
	if len(healthy) == 0 {
		return nil, ErrNoHealthyNodes
	}

	h := sha256.Sum256([]byte(key))
	val := binary.BigEndian.Uint64(h[:8])
	idx := int(val % uint64(len(healthy)))
	return healthy[idx], nil
}

// RoundRobinServer selects an active storage server in round-robin sequence.
func (a *App) RoundRobinServer() (*fileserver.FileServer, error) {
	healthy := a.GetHealthyServers()
	if len(healthy) == 0 {
		return nil, ErrNoHealthyNodes
	}

	idx := int(atomic.AddUint64(&a.rrIndex, 1) % uint64(len(healthy)))
	return healthy[idx], nil
}
