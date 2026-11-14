package fileserver

import (
	"io"
	"log"
	"sync"
	"time"

	"github.com/yigithankarabulut/distributed-file-storage/store"
)

// GDriveBackupJob represents an asynchronous Google Drive replication task.
type GDriveBackupJob struct {
	Key         string
	Filename    string
	Attempts    int
	MaxAttempts int
}

// GDriveQueue implements a resilient asynchronous backup queue with exponential backoff and retry.
type GDriveQueue struct {
	server      *FileServer
	gdriveStore *store.GDriveStore
	jobs        chan GDriveBackupJob
	done        chan struct{}
	wg          sync.WaitGroup
	stopped     bool
	mu          sync.Mutex
}

// NewGDriveQueue initializes and starts the async Google Drive queue worker.
func NewGDriveQueue(s *FileServer, gs *store.GDriveStore, bufferSize int) *GDriveQueue {
	if bufferSize <= 0 {
		bufferSize = 256
	}
	q := &GDriveQueue{
		server:      s,
		gdriveStore: gs,
		jobs:        make(chan GDriveBackupJob, bufferSize),
		done:        make(chan struct{}),
	}
	q.Start()
	return q
}

// Start launches the background worker goroutine.
func (q *GDriveQueue) Start() {
	q.wg.Add(1)
	go q.worker()
}

// Stop gracefully stops the backup queue worker.
func (q *GDriveQueue) Stop() {
	q.mu.Lock()
	if q.stopped {
		q.mu.Unlock()
		return
	}
	q.stopped = true
	close(q.done)
	q.mu.Unlock()
	q.wg.Wait()
}

// Enqueue submits a file to the asynchronous Google Drive backup queue.
func (q *GDriveQueue) Enqueue(key string, filename string) {
	if q.gdriveStore == nil || !q.gdriveStore.Enabled {
		if q.server.Metadata != nil {
			_ = q.server.Metadata.UpdateGDriveStatus(key, "disabled", "", "")
		}
		return
	}

	if q.server.Metadata != nil {
		_ = q.server.Metadata.UpdateGDriveStatus(key, "pending", "", "")
	}

	q.mu.Lock()
	stopped := q.stopped
	q.mu.Unlock()
	if stopped {
		return
	}

	select {
	case q.jobs <- GDriveBackupJob{Key: key, Filename: filename, Attempts: 0, MaxAttempts: 3}:
	default:
		log.Printf("[%s] warning: GDrive backup queue at capacity, dropping immediate enqueue for %s\n", q.server.ListenAddr, key)
	}
}

func (q *GDriveQueue) worker() {
	defer q.wg.Done()
	for {
		select {
		case <-q.done:
			return
		case job := <-q.jobs:
			q.processJob(job)
		}
	}
}

func (q *GDriveQueue) processJob(job GDriveBackupJob) {
	_, encReader, err := q.server.Storage.Read(q.server.ID, job.Key)
	if err != nil {
		log.Printf("[%s] GDrive worker: cannot read storage file for key (%s): %v\n", q.server.ListenAddr, job.Key, err)
		if q.server.Metadata != nil {
			_ = q.server.Metadata.UpdateGDriveStatus(job.Key, "failed", "", err.Error())
		}
		return
	}
	defer func() {
		if rc, ok := encReader.(io.ReadCloser); ok {
			_ = rc.Close()
		}
	}()

	driveFileID, uploadErr := q.gdriveStore.UploadFile(job.Key, job.Filename, encReader)
	if uploadErr != nil {
		job.Attempts++
		if job.Attempts < job.MaxAttempts {
			// Exponential backoff: 500ms, 1s, 2s...
			backoff := time.Duration(1<<job.Attempts) * 500 * time.Millisecond
			log.Printf("[%s] GDrive upload attempt %d failed for (%s): %v. Retrying in %v...\n",
				q.server.ListenAddr, job.Attempts, job.Filename, uploadErr, backoff)
			go func(j GDriveBackupJob, d time.Duration) {
				time.Sleep(d)
				q.mu.Lock()
				stopped := q.stopped
				q.mu.Unlock()
				if !stopped {
					select {
					case q.jobs <- j:
					case <-q.done:
					}
				}
			}(job, backoff)
			return
		}

		log.Printf("[%s] GDrive upload failed permanently after %d attempts for (%s): %v\n",
			q.server.ListenAddr, job.Attempts, job.Filename, uploadErr)
		if q.server.Metadata != nil {
			_ = q.server.Metadata.UpdateGDriveStatus(job.Key, "failed", "", uploadErr.Error())
		}
		return
	}

	log.Printf("[%s] GDrive upload succeeded for (%s). Drive File ID: %s\n", q.server.ListenAddr, job.Filename, driveFileID)
	if q.server.Metadata != nil {
		_ = q.server.Metadata.UpdateGDriveStatus(job.Key, "synced", driveFileID, "")
	}
}
