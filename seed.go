package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

type InfoDict struct {
	Name        string `bencode:"name"`
	PieceLength int    `bencode:"piece length"`
	Pieces      []byte `bencode:"pieces"`
	Length      int64  `bencode:"length"`
}

func CalcInfoHashFromFile(filePath string, pieceLength int64) (string, error) {
	mi := metainfo.MetaInfo{}
	info := metainfo.Info{
		PieceLength: pieceLength,
	}
	err := info.BuildFromFilePath(filePath)
	if err != nil {
		log.Println("Cannot build metainfo from file: ", err)
		return "", err
	}
	mi.InfoBytes, err = bencode.Marshal(info)
	return mi.HashInfoBytes().HexString(), nil
}

func SeedFromFile(digest, filePath string, client *torrent.Client) error {
	trackerAnnounceURL := os.Getenv("TRACKER_ANNOUNCE_URL")
	if trackerAnnounceURL == "" {
		trackerAnnounceURL = "http://chihaya-service:6969/announce"
		log.Println("TRACKER_ANNOUNCE_URL is not set, using default:", trackerAnnounceURL)
	}

	serviceInfohash, err := GetInfohash(digest)
	if err != nil {
		log.Println("Error getting infohash:", err)
		return err
	}

	if serviceInfohash == "" {
		log.Println("No infohash found for digest:", digest, "calculateing it now")
		infoHash, err := CalcInfoHashFromFile(filePath, 262144)
		if err != nil {
			log.Println("Error calculating infohash from file:", err)
			return err
		}
		if err := Modify(digest, infoHash); err != nil {
			log.Println("Error modifying infohash:", err)
			return err
		}
		serviceInfohash = infoHash
		log.Println("Successfully modified infohash for digest:", digest, "to", serviceInfohash)
	}
	t, err := client.AddMagnet(fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s&tr=%s", serviceInfohash, digest, trackerAnnounceURL))
	if err != nil {
		log.Println("Error adding magnet link:", err)
		return err
	}
	log.Println("Successfully added magnet link for digest:", digest, "with infohash:", serviceInfohash)
	<-t.GotInfo()
	log.Println("Starting to seed digest:", digest, "with infohash:", serviceInfohash)
	t.DownloadAll()
	return nil
}

func InitSeed(blobPath, algo string, m *BlobTaskManager) {
	entries, err := os.ReadDir(blobPath)
	if err != nil {
		log.Printf("Enable to read init seeding dir %s: %v", blobPath, err)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			digest := algo + ":" + entry.Name()
			srcPath := filepath.Join(blobPath, entry.Name())
			destPath := filepath.Join(m.dataDir, digest)
			err := os.Link(srcPath, destPath)
			if err != nil && !os.IsExist(err) {
				log.Println("Unable to create hard link for ", digest, ":", err)
			}
		}
	}
	entries, err = os.ReadDir(m.dataDir)
	if err != nil {
		log.Printf("Enable to read bt work dir %s: %v", m.dataDir, err)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			digest := entry.Name()
			infohash, err := GetInfohash(digest)
			if err != nil {
				log.Printf("Unable to get infohash for digest %s: %v", digest, err)
			}
			if infohash == "" {
				log.Printf("No infohash found for digest %s, calculating it now", digest)
				srcPath := filepath.Join(m.dataDir, entry.Name())
				infoHash, err := CalcInfoHashFromFile(srcPath, 262144)
				if err != nil {
					log.Printf("Unable to calculate infohash for digest %s: %v", digest, err)
					continue
				}
				infohash = infoHash
				if err := Modify(digest, infohash); err != nil {
					log.Printf("Unable to modify infohash for digest %s: %v", digest, err)
					continue
				}
			}
			m.AddTask(digest, infohash, "")
		}
	}
}
