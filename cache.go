package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"time"
)

type HTTPCache struct {
	path string
}

func (c *HTTPCache) fetch(method, url string) (bytes []byte, header http.Header, err error) {
	var resp *http.Response

	switch method {
	case "GET":
		resp, err = http.Get(url)
		if err != nil {
			return nil, nil, err
		}

		var reader io.ReadCloser
		// Check that the server actually sent compressed data
		switch resp.Header.Get("Content-Encoding") {
		case "gzip":
			reader, err = gzip.NewReader(resp.Body)
			if err != nil {
				return nil, nil, err
			}
			defer reader.Close()
		default:
			reader = resp.Body
		}

		bytes, err = ioutil.ReadAll(reader)
		if err != nil {
			return nil, nil, err
		}

		fallthrough
	case "HEAD":
		resp, err = http.Head(url)
		if err != nil {
			return nil, nil, err
		}

		header = resp.Header
	default:
		return nil, nil, fmt.Errorf("invalid method '%s'", method)
	}

	if resp.StatusCode >= 400 {
		return bytes, header, fmt.Errorf("%d: %s", resp.StatusCode, resp.Status)
	}

	return bytes, header, nil
}

func (c *HTTPCache) getLocalPath(url *url.URL) string {
	path := fmt.Sprintf("%s/%s/%s", c.path, url.Host, url.Path)
	if len(url.Query()) > 0 {
		path += "?" + url.Query().Encode()
	}
	return path
}

func (c *HTTPCache) readHeader(path string) (http.Header, error) {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var header http.Header
	if err := json.Unmarshal(content, &header); err != nil {
		return nil, err
	}

	return header, nil
}

func (c *HTTPCache) readContent(path string) ([]byte, error) {
	return ioutil.ReadFile(path)
}

func (c *HTTPCache) writeContent(path string, content []byte) error {
	return ioutil.WriteFile(path, content, 0644)
}

func (c *HTTPCache) writeHeader(path string, header http.Header) error {
	jsonHeader, err := json.Marshal(header)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(path, jsonHeader, 0644)
}

func hasExpired(path string) bool {
	if fileInfo, err := os.Stat(path); err == nil {
		return fileInfo.ModTime().Before(time.Now().Add(-time.Hour * 24))
	}
	return true
}

func (c *HTTPCache) request(method string, rawurl string) (content []byte, header http.Header, _ error) {
	url, err := url.Parse(rawurl)
	if err != nil {
		return nil, nil, err
	}
	localPath := path.Join(c.getLocalPath(url), method)

	if contentPath := path.Join(localPath, "content"); !hasExpired(contentPath) {
		content, _ = c.readContent(contentPath)
	}
	if headerPath := path.Join(localPath, "headers"); !hasExpired(headerPath) {
		header, _ = c.readHeader(headerPath)
	}

	if (method == "GET" && content == nil) || header == nil {
		content, header, err = c.fetch(method, rawurl)
		if err != nil {
			return nil, nil, err
		}
		if err := c.Put(method, url, content, header); err != nil {
			return nil, nil, err
		}
	}
	return content, header, nil
}

func (c *HTTPCache) Get(rawurl string) ([]byte, http.Header, error) {
	return c.request("GET", rawurl)
}

func (c *HTTPCache) Head(rawurl string) ([]byte, http.Header, error) {
	return c.request("HEAD", rawurl)
}

func (c *HTTPCache) Put(method string, url *url.URL, content []byte, header http.Header) error {
	localPath := path.Join(c.getLocalPath(url), method)
	if err := os.MkdirAll(localPath, 0755); err != nil {
		return err
	}

	if err := c.writeContent(path.Join(localPath, "content"), content); err != nil {
		return err
	}

	return c.writeHeader(path.Join(localPath, "headers"), header)
}

func NewHTTPCache(path string) *HTTPCache {
	return &HTTPCache{
		path: path,
	}
}
