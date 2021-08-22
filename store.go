package main

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/gorilla/feeds"
)

type Store struct {
	httpCache *HTTPCache
	rssCache  string
}

func (s *Store) getCachedRSS(crawler, name string) (*feeds.RssFeedXml, error) {
	cachedPath := s.getRSSPath(crawler, name)

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

func (s *Store) RSS(crawler, name string) (string, error) {
	emission, err := s.Get(crawler, name)
	if err != nil {
		return "", err
	}

	rss, err := emission.RSS()
	if err != nil {
		return "", err
	}

	return rss, nil
}

func (s *Store) getRSSPath(crawler, name string) string {
	return path.Join(s.rssCache, crawler, name+".rss")
}

func (s *Store) loadEmission(crawlerName, name string) (*Emission, error) {
	crawler := getCrawler(crawlerName)
	if crawler == nil {
		return nil, fmt.Errorf("could not find crawler for %s", crawlerName)
	}

	emission := NewEmission(name, crawler, s.httpCache)

	emission.cachedRss, _ = s.getCachedRSS(crawlerName, name)

	if err := emission.Fetch(); err != nil {
		return nil, err
	}

	return emission, nil
}

func (s *Store) Get(crawler, name string) (*Emission, error) {
	emission, err := s.loadEmission(crawler, name)
	if err != nil {
		return nil, err
	}

	rss, err := emission.RSS()
	if err != nil {
		return nil, err
	}

	cachedPath := s.getRSSPath(crawler, name)

	if err := os.MkdirAll(path.Dir(cachedPath), 0711); err != nil {
		return nil, err
	}

	if err := ioutil.WriteFile(cachedPath, []byte(rss), 0644); err != nil {
		return nil, err
	}

	return emission, nil
}

func (s *Store) List() ([]*Emission, error) {
	crawlerInfos, err := ioutil.ReadDir(s.rssCache)
	if err != nil {
		return nil, err
	}

	var emissions []*Emission
	for _, crawlerInfo := range crawlerInfos {
		crawler := path.Base(crawlerInfo.Name())
		fileInfos, err := ioutil.ReadDir(path.Join(s.rssCache, crawlerInfo.Name()))
		if err != nil {
			return nil, err
		}

		for _, fileInfo := range fileInfos {
			if strings.HasSuffix(fileInfo.Name(), ".rss") {
				name := strings.TrimSuffix(path.Base(fileInfo.Name()), ".rss")
				emission, err := s.loadEmission(crawler, name)
				if err == nil {
					emissions = append(emissions, emission)
				}
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
