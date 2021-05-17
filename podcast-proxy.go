package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/feeds"
	"golang.org/x/net/html"
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

type Emission struct {
	name      string
	cache     *HTTPCache
	feed      *feeds.Feed
	cachedRss *feeds.RssFeedXml
}

func (e *Emission) RSS() (string, error) {
	return e.feed.ToRss()
}

func (e *Emission) parseRSS(url string) (*feeds.RssFeedXml, error) {
	var rssFeed feeds.RssFeedXml

	bytes, _, err := e.cache.Get(url)
	if err != nil {
		return nil, err
	}

	if err := xml.Unmarshal(bytes, &rssFeed); err != nil {
		return nil, err
	}

	return &rssFeed, nil
}

func getAttribute(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func (e *Emission) parseFigure(node *html.Node, templateItem *feeds.RssItem) (*feeds.Item, error) {
	var button *html.Node
	buttons := htmlquery.Find(node, "//button")
	for _, button = range buttons {
		if getAttribute(button, "data-broadcast-type") == "replay" &&
			getAttribute(button, "data-is-aod") == "1" {
			break
		}
	}
	if button == nil {
		return nil, errors.New("failed to find button")
	}

	title := getAttribute(button, "data-diffusion-title")
	contentURL := getAttribute(button, "data-url")
	if !strings.HasSuffix(contentURL, ".mp3") {
		return nil, fmt.Errorf("unsupported media: %s", contentURL)
	}
	startTimeEpoch, _ := strconv.Atoi(getAttribute(button, "data-start-time"))
	startTime := time.Unix(int64(startTimeEpoch), 0)

	link := htmlquery.FindOne(node, "//a[1]")
	if link == nil {
		return nil, errors.New("failed to find link")
	}
	href := getAttribute(link, "href")

	return &feeds.Item{
		Title: title,
		Link: &feeds.Link{
			Href: href,
		},
		Author: &feeds.Author{
			Name: templateItem.Author,
		},
		Enclosure: &feeds.Enclosure{
			Url:  contentURL,
			Type: "audio/mpeg",
		},
		Created: startTime,
	}, nil
}

func (e *Emission) parsePage(url string, templateItem *feeds.RssItem) ([]*feeds.Item, error) {
	content, _, err := e.cache.Get(url)
	if err != nil {
		return nil, err
	}

	doc, err := html.Parse(bytes.NewBuffer(content))
	if err != nil {
		return nil, err
	}

	var items []*feeds.Item
	figures := htmlquery.Find(doc, "//figure")
	for _, figure := range figures {
		item, err := e.parseFigure(figure, templateItem)
		if err != nil {
			fmt.Printf("failed to parse figure: %s\n", err)
			continue
		}
		items = append(items, item)
	}

	return items, nil
}

func itemFromRss(rssItem *feeds.RssItem) *feeds.Item {
	createdAt, _ := time.Parse(time.RFC822, rssItem.PubDate)
	return &feeds.Item{
		Title: rssItem.Title,
		Link: &feeds.Link{
			Href: rssItem.Link,
		},
		Author: &feeds.Author{
			Name: rssItem.Author,
		},
		Enclosure: &feeds.Enclosure{
			Url:    rssItem.Enclosure.Url,
			Length: rssItem.Enclosure.Length,
			Type:   rssItem.Enclosure.Type,
		},
		Description: rssItem.Description,
		Created:     createdAt,
	}
}

func (e *Emission) Fetch() error {
	url := fmt.Sprintf("https://www.franceinter.fr/emissions/%s", e.name)
	content, _, err := e.cache.Get(url)
	if err != nil {
		return err
	}

	doc, err := html.Parse(bytes.NewBuffer(content))
	if err != nil {
		return err
	}

	rssLink := htmlquery.Find(doc, "//a[@class='podcast-button rss']")
	if len(rssLink) == 0 {
		return errors.New("failed to find RSS feed")
	}

	var rssFeed string
	for _, attr := range rssLink[0].Attr {
		if attr.Key == "href" {
			rssFeed = attr.Val
		}
	}

	if rssFeed == "" {
		return errors.New("failed to find RSS feed")
	}

	feed, err := e.parseRSS(rssFeed)
	if err != nil {
		return err
	}

	if len(feed.Channel.Items) == 0 {
		return errors.New("empty RSS feed")
	}

	e.feed = &feeds.Feed{
		Title:       feed.Channel.Title,
		Description: feed.Channel.Description,
		Copyright:   feed.Channel.Copyright,
		Link: &feeds.Link{
			Href: feed.Channel.Link,
		},
		Image: &feeds.Image{
			Url:    feed.Channel.Image.Url,
			Title:  feed.Channel.Image.Title,
			Link:   feed.Channel.Image.Link,
			Width:  feed.Channel.Image.Width,
			Height: feed.Channel.Image.Height,
		},
	}

	if lastBuildDate, err := time.Parse(time.RFC822, feed.Channel.LastBuildDate); err != nil {
		e.feed.Updated = lastBuildDate
	}

	cachedItems := make(map[string]*feeds.RssItem)
	if e.cachedRss != nil {
		for _, cachedItem := range e.cachedRss.Channel.Items {
			cachedItems[cachedItem.Link] = cachedItem
		}

		defer func() {
			for _, previousEntry := range e.cachedRss.Channel.Items {
				e.feed.Items = append(e.feed.Items, itemFromRss(previousEntry))
			}
		}()
	}

	for page := 1; page <= len(htmlquery.Find(doc, "//li[@class='pager-item']/a"))+1; page++ {
		entries, err := e.parsePage(fmt.Sprintf("%s?p=%d", url, page), feed.Channel.Items[0])
		if err != nil {
			return err
		}

		for _, entry := range entries {
			if _, found := cachedItems[entry.Link.Href]; found {
				return nil
			}

			// Get item description
			content, _, err := e.cache.Get(entry.Link.Href)
			if err != nil {
				return nil
			}

			doc, err := html.Parse(bytes.NewBuffer(content))
			if err != nil {
				return err
			}

			description := htmlquery.Find(doc, "//article/p[@class='chapo']")
			if len(description) > 0 {
				entry.Description = strings.TrimSpace(description[0].FirstChild.Data)
			} else {
				log.Printf("failed to find article for %s\n", entry.Link.Href)
			}

			// Get media length
			_, header, err := e.cache.Head(entry.Enclosure.Url)
			if err != nil {
				return err
			}
			entry.Enclosure.Length = header.Get("Content-Length")

			e.feed.Items = append(e.feed.Items, entry)
		}
	}

	return nil
}

func NewEmission(name string, httpCache *HTTPCache) *Emission {
	return &Emission{
		name:  name,
		cache: httpCache,
	}
}

type Store struct {
	httpCache *HTTPCache
	rssCache  string
}

func (s *Store) getCachedRSS(name string) (*feeds.RssFeedXml, error) {
	cachedPath := s.getRSSPath(name)

	content, err := ioutil.ReadFile(cachedPath)
	if err != nil {
		return nil, err
	}

	cachedRss := new(feeds.RssFeedXml)
	if err := xml.Unmarshal(content, cachedRss); err != nil {
		return nil, err
	}

	return cachedRss, nil
}

func (s *Store) RSS(name string) (string, error) {
	emission, err := s.Get(name)
	if err != nil {
		return "", err
	}

	rss, err := emission.RSS()
	if err != nil {
		return "", err
	}

	cachedPath := s.getRSSPath(name)
	if err := ioutil.WriteFile(cachedPath, []byte(rss), 0644); err != nil {
		return "", err
	}

	return rss, nil
}

func (s *Store) getRSSPath(name string) string {
	return path.Join(s.rssCache, name+".rss")
}

func (s *Store) loadEmission(name string) (*Emission, error) {
	emission := NewEmission(name, s.httpCache)

	emission.cachedRss, _ = s.getCachedRSS(name)

	if err := emission.Fetch(); err != nil {
		return nil, err
	}

	return emission, nil
}

func (s *Store) Get(name string) (*Emission, error) {
	return s.loadEmission(name)
}

func (s *Store) List() ([]*Emission, error) {
	fileInfos, err := ioutil.ReadDir(s.rssCache)
	if err != nil {
		return nil, err
	}

	var emissions []*Emission
	for _, fileInfo := range fileInfos {
		if strings.HasSuffix(fileInfo.Name(), ".rss") {
			name := strings.TrimSuffix(path.Base(fileInfo.Name()), ".rss")
			emission, err := s.loadEmission(name)
			if err == nil {
				emissions = append(emissions, emission)
			}
		}
	}

	return emissions, nil
}

func NewStore(dir string) (*Store, error) {
	rssCache := path.Join(dir, "rss")

	if err := os.MkdirAll(rssCache, 0755); err != nil {
		return nil, err
	}

	return &Store{
		httpCache: NewHTTPCache(path.Join(dir, "cache")),
		rssCache:  rssCache,
	}, nil
}

func main() {
	var listenPort int
	var listenAddr string

	flag.IntVar(&listenPort, "port", 8080, "port to listen on")
	flag.StringVar(&listenAddr, "addr", "0.0.0.0", "listen IP to bind")
	flag.Parse()

	store, err := NewStore(path.Join(os.Getenv("HOME"), ".cache", "podcast-proxy"))
	if err != nil {
		panic(err)
	}

	r := gin.Default()
	r.GET("/emissions", func(c *gin.Context) {
		emissions, err := store.List()
		if err != nil {
			c.String(http.StatusInternalServerError, fmt.Sprintf("error: %s", err))
			return
		}

		html := `<html><body><ul>`
		for _, emission := range emissions {
			html += "<p>"
			if image := emission.feed.Image; image != nil {
				html += fmt.Sprintf("<img src=\"%s\" width=50 style=\"vertical-align: middle\"/>", image.Url)
			}
			html += fmt.Sprintf("&nbsp;<a href='/emissions/%s/rss'>%s</a>", emission.name, emission.feed.Title)
			html += "</p>"
		}
		html += `</ul></body></html>`

		c.Data(200, "text/html; charset=utf-8", []byte(html))
	})
	r.GET("/emissions/:name/rss", func(c *gin.Context) {
		name := c.Param("name")
		rss, err := store.RSS(name)
		if err != nil {
			c.String(http.StatusInternalServerError, fmt.Sprintf("error: %s", err))
			return
		}
		c.Data(200, "application/xml; charset=utf-8", []byte(rss))
	})

	r.Run(fmt.Sprintf("%s:%d", listenAddr, listenPort))
}
