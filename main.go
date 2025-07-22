package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/anacrolix/torrent"
	"github.com/gin-gonic/gin"
)

func main() {
	listenPort := "2030"
	btDir := os.Getenv("BT_DIR")
	if btDir == "" {
		btDir = "/var/lib/bt-agent"
		log.Println("BT_DIR is not set, using default: /var/lib/bt-agent")
	}
	blobDir := os.Getenv("BLOB_DIR")
	if blobDir == "" {
		blobDir = "/var/lib/containerd/io.containerd.content.v1.content/blobs/sha256"
		log.Println("BLOB_DIR is not set, using default: /var/lib/containerd/io.containerd.content.v1.content/blobs/sha256")
	}
	registryURL := os.Getenv("REGISTRY_URL")
	if registryURL == "" {
		registryURL = "https://registry-1.docker.io"
		log.Println("REGISTRY_URL is not set, using default: https://registry-1.docker.io")
	}

	err := os.MkdirAll(btDir, 0755)
	if err != nil && !os.IsExist(err) {
		log.Fatalf("Failed to create BT directory %s: %v", btDir, err)
	}

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = btDir
	cfg.Seed = true
	client, err := torrent.NewClient(cfg)
	if err != nil {
		log.Fatalf("Failed to create torrent client: %v", err)
	}
	defer client.Close()
	blobManager := NewBlobTaskManager(client, btDir, registryURL)

	InitSeed(blobDir, "sha256", blobManager)

	remoteURL, err := url.Parse("https://" + registryURL)
	if err != nil {
		log.Fatalf("Failed to parse registry URL: %v", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(remoteURL)
	proxy.Director = func(req *http.Request) {
		log.Printf("Proxy request: %s %s", req.Method, req.URL.Path)
		req.URL.Scheme = remoteURL.Scheme
		req.URL.Host = remoteURL.Host
		req.Host = remoteURL.Host
	}
	proxyHandler := func(c *gin.Context) {
		proxy.ServeHTTP(c.Writer, c.Request)
	}

	router := gin.Default()
	v2 := router.Group("/v2")
	{
		// why cannot use /*repo/blobs/:digest wtf
		v2.Any("/*path", func(c *gin.Context) {
			path := c.Param("path")
			if strings.Contains(path, "/blobs/") {
				idx := strings.LastIndex(path, "/blobs/")
				repo := ""
				if path[0] == '/' {
					repo = path[1:idx]
				} else {
					repo = path[:idx]
				}
				digest := path[idx+len("/blobs/"):]
				if repo == "" || digest == "" {
					proxyHandler(c)
					return
				}
				handleGetBlob(c, blobManager, digest, repo)
				return
			}
			proxyHandler(c)
		})
	}
	router.Run(":" + listenPort)
}
