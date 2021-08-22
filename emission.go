package main

import (
	"encoding/xml"
	"time"

	"github.com/gorilla/feeds"
	"golang.org/x/net/html"
)

type Emission struct {
	name      string
	cache     *HTTPCache
	crawler   Crawler
	feed      *feeds.Feed
	cachedRss *feeds.RssFeedXml
}

func (e *Emission) RSS() (string, error) {
	return e.feed.ToRss()
}

func parseRSS(cache *HTTPCache, url string) (*feeds.RssFeedXml, error) {
	var rssFeed feeds.RssFeedXml

	bytes, _, err := cache.Get(url)
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
	feed, err := e.crawler.Fetch(e.name, e.cachedRss)
	if err != nil {
		return err
	}

	e.feed = feed

	return nil
}

func NewEmission(name string, crawler Crawler, httpCache *HTTPCache) *Emission {
	return &Emission{
		name:    name,
		crawler: crawler,
		cache:   httpCache,
	}
}
