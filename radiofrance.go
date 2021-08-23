package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	"github.com/gorilla/feeds"
	"golang.org/x/net/html"
)

type RadioFranceCrawler struct {
	cache                 *HTTPCache
	buttonIsFigureSibling bool
	url                   string
	episodeSelector       string
}

func (c *RadioFranceCrawler) getFeed(url string, doc *html.Node, rssFeedLink string, cachedRss *feeds.RssFeedXml) (*feeds.Feed, error) {
	var (
		feed         *feeds.Feed
		rssFeed      *feeds.RssFeedXml
		templateItem *feeds.RssItem
		err          error
	)

	if rssFeedLink != "" {
		feed, rssFeed, err = c.fetchRSSFeed(rssFeedLink)
		if err != nil {
			log.Printf("Failed to reach rss feed: %s", err)
		}

		if rssFeed != nil {
			if len(rssFeed.Channel.Items) == 0 {
				return nil, errors.New("empty RSS feed")
			}

			templateItem = rssFeed.Channel.Items[0]
		}
	}

	if feed == nil {
		var description, title, image string

		if meta := htmlquery.FindOne(doc, "//meta[@name='description']"); meta != nil {
			for _, attr := range meta.Attr {
				if attr.Key == "content" {
					description = attr.Val
				}
			}
		}

		if titleNode := htmlquery.FindOne(doc, "//title"); titleNode != nil {
			title = titleNode.FirstChild.Data
		}

		if imageNode := htmlquery.FindOne(doc, "//div[@class='cover-picture']//img"); imageNode != nil {
			for _, attr := range imageNode.Attr {
				if attr.Key == "data-dejavu-src" {
					image = attr.Val
				}
			}
		}

		rssFeed = &feeds.RssFeedXml{
			Channel: &feeds.RssFeed{
				Title:       title,
				Description: description,
				Link:        url,
				Image: &feeds.RssImage{
					Url:   image,
					Title: title,
					Link:  image,
				},
				Items: []*feeds.RssItem{
					{},
				},
			},
		}

		templateItem = rssFeed.Channel.Items[0]

		feed = c.feedFromRSSFeed(rssFeed)
	}

	cachedItems := make(map[string]*feeds.RssItem)
	if cachedRss != nil {
		for _, cachedItem := range cachedRss.Channel.Items {
			cachedItems[cachedItem.Enclosure.Url] = cachedItem
		}

		defer func() {
			for _, previousEntry := range cachedRss.Channel.Items {
				feed.Items = append(feed.Items, itemFromRss(previousEntry))
			}
		}()
	}

	pages := c.getPages(doc)
	for page := 1; page <= len(pages)+1; page++ {
		entries, err := c.parsePage(fmt.Sprintf("%s?p=%d", url, page), templateItem)
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			if _, found := cachedItems[entry.Enclosure.Url]; found {
				return feed, nil
			}

			if entry.Description = c.getItemDescription(entry); len(entry.Description) == 0 {
				log.Printf("failed to find article for %s\n", entry.Id)
			}

			// Get media length
			_, header, err := c.cache.Head(entry.Enclosure.Url)
			if err != nil {
				return nil, err
			}
			entry.Enclosure.Length = header.Get("Content-Length")

			feed.Items = append(feed.Items, entry)
		}
	}

	return feed, nil
}

func (c *RadioFranceCrawler) feedFromRSSFeed(rssFeed *feeds.RssFeedXml) *feeds.Feed {
	return &feeds.Feed{
		Title:       rssFeed.Channel.Title,
		Description: rssFeed.Channel.Description,
		Copyright:   rssFeed.Channel.Copyright,
		Link: &feeds.Link{
			Href: rssFeed.Channel.Link,
		},
		Image: &feeds.Image{
			Url:    rssFeed.Channel.Image.Url,
			Title:  rssFeed.Channel.Image.Title,
			Link:   rssFeed.Channel.Image.Link,
			Width:  rssFeed.Channel.Image.Width,
			Height: rssFeed.Channel.Image.Height,
		},
	}
}

func (c *RadioFranceCrawler) getPages(doc *html.Node) []*html.Node {
	return htmlquery.Find(doc, "//li[@class='pager-item']/a")
}

func (c *RadioFranceCrawler) getItemDescription(entry *feeds.Item) string {
	if entry.Link == nil {
		return ""
	}

	// Get item description
	content, _, err := c.cache.Get(entry.Link.Href)
	if err != nil {
		return ""
	}

	doc, err := html.Parse(bytes.NewBuffer(content))
	if err != nil {
		return ""
	}

	description := htmlquery.Find(doc, "//article//p")
	if len(description) > 0 {
		return strings.TrimSpace(description[0].FirstChild.Data)
	}

	return ""

}

func (c *RadioFranceCrawler) fetchRSSFeed(rssFeedLink string) (*feeds.Feed, *feeds.RssFeedXml, error) {
	rssFeed, err := parseRSS(c.cache, rssFeedLink)
	if err != nil {
		return nil, nil, err
	}

	if len(rssFeed.Channel.Items) == 0 {
		return nil, nil, errors.New("empty RSS feed")
	}

	feed := &feeds.Feed{
		Title:       rssFeed.Channel.Title,
		Description: rssFeed.Channel.Description,
		Copyright:   rssFeed.Channel.Copyright,
		Link: &feeds.Link{
			Href: rssFeed.Channel.Link,
		},
		Image: &feeds.Image{
			Url:    rssFeed.Channel.Image.Url,
			Title:  rssFeed.Channel.Image.Title,
			Link:   rssFeed.Channel.Image.Link,
			Width:  rssFeed.Channel.Image.Width,
			Height: rssFeed.Channel.Image.Height,
		},
	}

	if lastBuildDate, err := time.Parse(time.RFC822, rssFeed.Channel.LastBuildDate); err != nil {
		feed.Updated = lastBuildDate
	}

	return feed, rssFeed, nil
}

func (c *RadioFranceCrawler) parseEpisode(node *html.Node, templateItem *feeds.RssItem) (*feeds.Item, error) {
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

	var href string
	link := htmlquery.FindOne(node, "//a[1]")
	if link != nil {
		href = getAttribute(link, "href")

		if !strings.HasPrefix(href, "https://") {
			href = c.url + href
		}
	}

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

func (c *RadioFranceCrawler) parsePage(url string, templateItem *feeds.RssItem) ([]*feeds.Item, error) {
	content, _, err := c.cache.Get(url)
	if err != nil {
		return nil, err
	}

	doc, err := html.Parse(bytes.NewBuffer(content))
	if err != nil {
		return nil, err
	}

	var items []*feeds.Item
	buttonIsFigureSibling := false
	episodes := htmlquery.Find(doc, "//div[@class='podcast-list']/div")
	if len(episodes) == 0 {
		episodes = htmlquery.Find(doc, "//figure")
		buttonIsFigureSibling = c.buttonIsFigureSibling
	}

	for _, episode := range episodes {
		if buttonIsFigureSibling {
			episode = episode.Parent.Parent
		}

		item, err := c.parseEpisode(episode, templateItem)
		if err != nil {
			fmt.Printf("failed to parse figure: %s\n", err)
			continue
		}
		items = append(items, item)
	}

	return items, nil
}
