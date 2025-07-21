package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	www_authenticate_parser "github.com/gboddin/go-www-authenticate-parser"
	"github.com/gin-gonic/gin"
)

func P2PDownload(c *gin.Context, task *BlobTask) {
	task.mu.Lock()
	t := task.Torrent
	task.mu.Unlock()

	if t == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "P2P download failed, torrent is nil"})
		return
	}

	f := t.Files()[0]
	r := f.NewReader()
	defer r.Close()
	c.Header("Content-Disposition", "attachment; filename="+f.DisplayPath())
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", fmt.Sprintf("%d", f.Length()))
	c.Header("Docker-Content-Digest", task.Digest)
	if _, err := io.Copy(c.Writer, r); err != nil {
		log.Printf("Failed to write file %s to response: %v", f.DisplayPath(), err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	return
}

func GetRegistryAuthToken(registryURL, repoName string) (string, error) {
	scope := fmt.Sprintf("repository:%s:pull", repoName)
	probeURL := fmt.Sprintf("https://%s/v2/", registryURL)
	resp, err := http.Get(probeURL)
	if err != nil {
		log.Printf("Failed to probe registry %s: %v", probeURL, err)
		return "", errors.New("Failed to probe registry")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		if resp.StatusCode == http.StatusOK {
			return "", nil
		}
		return "", fmt.Errorf("Failed to probe registry %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	header := resp.Header.Get("WWW-Authenticate")
	if header == "" {
		return "", fmt.Errorf("Failed to get registry response header")
	}
	setting := www_authenticate_parser.Parse(header)
	params := setting.Params
	tokenReqURL := fmt.Sprintf("%s?service=%s&scope=%s", params["realm"], params["service"], scope)
	tokenResp, err := http.Get(tokenReqURL)
	if err != nil {
		return "", fmt.Errorf("Enable to request token %s: %w", tokenReqURL, err)
	}
	defer tokenResp.Body.Close()

	var tokenData struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	json.NewDecoder(tokenResp.Body).Decode(&tokenData)

	token := tokenData.Token
	if token == "" {
		token = tokenData.AccessToken
	}
	return token, nil
}

func FallbackDownload(c *gin.Context, m *BlobTaskManager, task *BlobTask) {
	token, err := GetRegistryAuthToken(m.registryURL, task.RepoName)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "fallback auth failed: " + err.Error()})
		return
	}

	downloadURL := fmt.Sprintf("https://%s/v2/%s/blobs/%s", m.registryURL, task.RepoName, task.Digest)
	req, _ := http.NewRequest("GET", downloadURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req.WithContext(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "fallback request failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadGateway, gin.H{"error": "fallback unexpected status: " + resp.Status})
		return
	}

	filePath := filepath.Join(m.dataDir, task.Digest)
	tempPath := filePath + ".tmp_download"
	outFile, err := os.Create(tempPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot create temp file: " + err.Error()})
		return
	}
	defer outFile.Close()

	teeReader := io.TeeReader(resp.Body, outFile)

	c.Header("Content-Length", resp.Header.Get("Content-Length"))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Docker-Content-Digest", task.Digest)

	if _, err = io.Copy(c.Writer, teeReader); err != nil {
		log.Printf("Failed to write file %s to response: %v", task.Digest, err)
		c.AbortWithStatus(http.StatusInternalServerError)
		os.Remove(tempPath)
		return
	}

	if err := os.Rename(tempPath, filePath); err != nil {
		log.Printf("%s fallback downloading failed to rename tmp file: %v", task.Digest, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "fallback downloading failed to rename tmp file: " + err.Error()})
		os.Remove(tempPath)
		return
	} else {
		task.mu.Lock()
		SeedFromFile(task.Digest, filePath, m.btClient)
		task.Status = BlobStatusSeed
		task.mu.Unlock()
	}
	return
}

func handleGetBlob(c *gin.Context, m *BlobTaskManager, digest, repoName string) {
	task := m.AddTask(digest, "", repoName)

	select {
	case <-task.TorrentReady:
		P2PDownload(c, task)
	case <-task.Fallback:
		FallbackDownload(c, m, task)
	case <-c.Request.Context().Done():
		log.Printf("Request for digest %s was cancelled", digest)
		c.AbortWithStatus(http.StatusRequestTimeout)
		return
	}
}
