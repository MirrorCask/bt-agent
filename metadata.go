package main

// access Metadata Service

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func GetInfohash(digest string) (string, error) {
	metaservice_url := os.Getenv("METASERVICE_URL")
	if metaservice_url == "" {
		metaservice_url = "http://metaservice.kube-system.svc.cluster.local:80"
		log.Println("METASERVICE_URL is not set, using default: %s", metaservice_url)
	}
	params := url.Values{}
	Url, err := url.Parse(metaservice_url)
	if err != nil {
		return "", err
	}
	params.Set("digest", digest)
	Url.Path = "/infohash"
	Url.RawQuery = params.Encode()
	resp, err := http.Get(Url.String())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusInternalServerError {
			log.Println("METASERVICE_URL may be unreachable, please check the service status")
			return "", errors.New("StatusInternalServerError")
		}
		if resp.StatusCode == http.StatusNotFound {
			log.Println("No infohash found for digest:", digest)
			return "", nil
		}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	jsonMap := make(map[string]interface{}, 0)
	if err := json.Unmarshal(body, &jsonMap); err != nil {
		return "", err
	}
	infohash, ok := jsonMap["infohash"].(string)
	if !ok {
		log.Printf("Bad response: expected infohash in JSON, got %s", string(body))
		return "", errors.New("Bad response: 200 but infohash not found")
	}
	return infohash, nil
}

func Modify(digest, infohash string) error {
	metaservice_url := os.Getenv("METASERVICE_URL")
	if metaservice_url == "" {
		metaservice_url = "http://metaservice.kube-system.svc.cluster.local:80"
		log.Printf("METASERVICE_URL is not set, using default: %s", metaservice_url)
	}
	params := url.Values{}
	params.Set("digest", digest)
	params.Set("infohash", infohash)
	Url, err := url.Parse(metaservice_url)
	if err != nil {
		return err
	}
	Url.Path = "/modify"
	Url.RawQuery = params.Encode()
	log.Println("Modifying infohash for digest:", digest, "to", infohash)
	resp, err := http.Post(Url.String(), "application/x-www-form-urlencoded", strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusInternalServerError {
			log.Println("METASERVICE_URL may be unreachable, please check the service status")
			return errors.New("StatusInternalServerError")
		}
		if resp.StatusCode == http.StatusBadRequest {
			log.Println("Bad request for digest:", digest, "and infohash:", infohash)
			return errors.New("StatusBadRequest")
		}
	}
	log.Println("Successfully modified infohash for digest:", digest, "to", infohash)
	return nil
}
