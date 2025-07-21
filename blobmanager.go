package main

import (
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/anacrolix/torrent"
)

type BlobTaskManager struct {
	Tasks       map[string]*BlobTask
	mu          sync.Mutex
	btClient    *torrent.Client
	dataDir     string
	registryURL string
}

func NewBlobTaskManager(btClient *torrent.Client, dataDir, registryURL string) *BlobTaskManager {
	return &BlobTaskManager{
		Tasks:       make(map[string]*BlobTask),
		btClient:    btClient,
		dataDir:     dataDir,
		registryURL: registryURL,
	}
}

func (m *BlobTaskManager) AddTask(digest, infohash, repoName string) *BlobTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	if task, exists := m.Tasks[digest]; exists {
		if m.Tasks[digest].RepoName == "" && repoName != "" {
			m.Tasks[digest].RepoName = repoName
		}
		if m.Tasks[digest].Infohash == "" && infohash != "" {
			m.Tasks[digest].Infohash = infohash
		}
		log.Printf("Task for digest %s already exists, skipping", digest)
		return task
	}
	task := NewBlobTask(digest, infohash, repoName)
	m.Tasks[digest] = task

	go m.RunTask(task)
	return task
}

func (m *BlobTaskManager) RunTask(task *BlobTask) {
	task.mu.Lock()
	defer task.mu.Unlock()
	infohash := task.Infohash
	if infohash == "" {
		infohash, err := GetInfohash(task.Digest)
		if err != nil {
			log.Printf("Error getting infohash for digest %s: %v", task.Digest)
			task.Status = BlobStatusError
			close(task.Fallback)
			return
		}
		if infohash == "" {
			log.Printf("No infohash found for digest %s, using directly downloading", task.Digest)
			close(task.Fallback)
			return
		}
		task.Infohash = infohash
	}

	task.mu.Unlock()
	trackerAnnouncement := os.Getenv("TRACKER_ANNOUNCEMENT")
	if trackerAnnouncement == "" {
		trackerAnnouncement = "http://tracker.kube-system.svc.cluster.local:80/announce"
		log.Println("TRACKER_ANNOUNCEMENT is not set, using default:", trackerAnnouncement)
	}
	task.mu.Lock()
	MagnetURL := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s&tr=%s", infohash, task.Digest, trackerAnnouncement)
	task.mu.Unlock()
	t, err := m.btClient.AddMagnet(MagnetURL)
	if err != nil {
		log.Printf("Error adding magnet for digest %s: %v", task.Digest, err)
		task.mu.Lock()
		task.Status = BlobStatusError
		close(task.Fallback)
		task.mu.Unlock()
		return
	}
	task.mu.Lock()
	task.Torrent = t
	task.mu.Unlock()

	select {
	case <-t.GotInfo():
		log.Printf("Torrent for digest %s is ready", task.Digest)
	case <-m.btClient.Closed():
		log.Printf("Torrent client closed while waiting for digest %s", task.Digest)
		task.mu.Lock()
		close(task.Fallback)
		task.mu.Unlock()
		return
	}
	t.DownloadAll()
	task.mu.Lock()
	task.Status = BlobStatusDownloadingBt
	close(task.TorrentReady)
	task.mu.Unlock()

	if t.Complete().Bool() {
		log.Printf("Torrent for digest %s is already completed, seeding", task.Digest)
		task.mu.Lock()
		task.Status = BlobStatusSeed
		task.mu.Unlock()
		return
	}

	select {
	case <-t.Complete().On():
		log.Printf("Torrent for digest %s has completed downloading", task.Digest)
		task.mu.Lock()
		task.Status = BlobStatusSeed
		task.mu.Unlock()
	case <-m.btClient.Closed():
		log.Printf("Torrent client closed while downloading digest %s", task.Digest)
		task.mu.Lock()
		task.Status = BlobStatusError
		close(task.Fallback)
		task.mu.Unlock()
	}
}
