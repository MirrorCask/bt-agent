package main

import (
	"sync"

	"github.com/anacrolix/torrent"
)

type BlobStatus int

const (
	BlobStatusInitializing BlobStatus = iota
	BlobStatusDownloadingBt
	BlobStatusDownloading
	BlobStatusSeed
	BlobStatusError
)

func (s BlobStatus) String() string {
	return []string{"Initializing", "DownloadingBt", "Downloading", "Seeding", "Error"}[s]
}

type BlobTask struct {
	Digest       string
	Infohash     string
	RepoName     string
	Status       BlobStatus
	Torrent      *torrent.Torrent
	TorrentReady chan struct{}
	Fallback     chan struct{}
	mu           sync.Mutex
}

func NewBlobTask(digest, infohash, repoName string) *BlobTask {
	return &BlobTask{
		Digest:       digest,
		RepoName:     repoName,
		Status:       BlobStatusInitializing,
		TorrentReady: make(chan struct{}),
		Infohash:     infohash,
	}
}
