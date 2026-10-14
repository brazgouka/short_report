// Package news fetches today's headlines for a set of user-chosen tags
// from a spread of public RSS feeds spanning different outlets and
// editorial perspectives (not just one newsroom's judgment of what
// matters), with a live-search fallback merged in for even broader
// source coverage, and a small in-memory cache so repeated requests
// (e.g. dragging a slider) don't refetch on every call.
package news

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Article is a single normalized headline.
type Article struct {
	Tag         string    `json:"tag"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Source      string    `json:"source"`
	Link        string    `json:"link"`
	Published   time.Time `json:"published"`
}

// curatedFeeds maps a lowercased tag to a set of known-good RSS feeds
// deliberately drawn from different outlets and editorial leanings
// (center, left, right, and international wire-style coverage), so no
// single newsroom decides what's "important" for a topic — that
// judgment is left to the LLM, working from all of it. A live Google
// News search is always merged in too (see FetchTag) for even broader
// coverage, especially for tags without a curated set below.
var curatedFeeds = map[string][]string{
	"world": {
		"https://feeds.bbci.co.uk/news/world/rss.xml",   // BBC (UK, wire-style)
		"https://www.theguardian.com/world/rss",         // The Guardian (UK, left)
		"https://www.aljazeera.com/xml/rss/all.xml",     // Al Jazeera (international)
		"https://feeds.skynews.com/feeds/rss/world.xml", // Sky News (UK, center)
		"https://www.france24.com/en/rss",               // France 24 (France, international)
		"http://rss.cnn.com/rss/cnn_world.rss",          // CNN (US, left-center)
	},
	"usa": {
		"https://feeds.bbci.co.uk/news/world/us_and_canada/rss.xml", // BBC (outside view)
		"https://feeds.npr.org/1001/rss.xml",                        // NPR (US, center-left)
		"https://moxie.foxnews.com/google-publisher/latest.xml",     // Fox News (US, right)
		"http://rss.cnn.com/rss/cnn_us.rss",                         // CNN (US, left-center)
		"https://nypost.com/feed/",                                  // NY Post (US, right)
		"https://www.cbsnews.com/latest/rss/us",                     // CBS News (US, center)
		"https://abcnews.go.com/abcnews/usheadlines",                // ABC News (US, center)
	},
	"us": {
		"https://feeds.bbci.co.uk/news/world/us_and_canada/rss.xml",
		"https://feeds.npr.org/1001/rss.xml",
		"https://moxie.foxnews.com/google-publisher/latest.xml",
		"http://rss.cnn.com/rss/cnn_us.rss",
		"https://nypost.com/feed/",
		"https://www.cbsnews.com/latest/rss/us",
		"https://abcnews.go.com/abcnews/usheadlines",
	},
	"politics": {
		"https://feeds.bbci.co.uk/news/politics/rss.xml",          // BBC
		"https://feeds.npr.org/1014/rss.xml",                      // NPR (US, center-left)
		"https://moxie.foxnews.com/google-publisher/politics.xml", // Fox News (US, right)
		"http://rss.cnn.com/rss/cnn_allpolitics.rss",              // CNN (US, left-center)
		"https://thehill.com/feed/",                               // The Hill (US, centrist/insider)
	},
	"new york": {
		"https://rss.nytimes.com/services/xml/rss/nyt/NYRegion.xml", // NYT (US, center-left)
		"https://nypost.com/feed/",                                  // NY Post (US, right)
		"https://gothamist.com/feed",                                // Gothamist (local/independent)
	},
	"business": {
		"https://feeds.bbci.co.uk/news/business/rss.xml",        // BBC
		"https://www.cnbc.com/id/100003114/device/rss/rss.html", // CNBC (US)
	},
	"technology": {
		"https://feeds.bbci.co.uk/news/technology/rss.xml", // BBC
		"https://techcrunch.com/feed/",                     // TechCrunch
		"https://feeds.arstechnica.com/arstechnica/index",  // Ars Technica
		"https://www.theverge.com/rss/index.xml",           // The Verge
	},
	"science": {
		"https://feeds.bbci.co.uk/news/science_and_environment/rss.xml", // BBC
		"https://www.nasa.gov/news-release/feed/",                       // NASA
		"https://www.sciencedaily.com/rss/all.xml",                      // Science Daily
	},
	"health": {
		"https://feeds.bbci.co.uk/news/health/rss.xml", // BBC
		"https://feeds.npr.org/1128/rss.xml",           // NPR Health
	},
	"soccer": {
		"https://feeds.bbci.co.uk/sport/football/rss.xml", // BBC Sport
		"https://www.skysports.com/rss/12040",             // Sky Sports
		"https://www.espn.com/espn/rss/soccer/news",       // ESPN
	},
	"football": {
		"https://feeds.bbci.co.uk/sport/football/rss.xml",
		"https://www.skysports.com/rss/12040",
		"https://www.espn.com/espn/rss/soccer/news",
	},
	"entertainment": {
		"https://feeds.bbci.co.uk/news/entertainment_and_arts/rss.xml", // BBC
		"https://variety.com/feed/",                                    // Variety
		"https://www.hollywoodreporter.com/feed/",                      // Hollywood Reporter
	},
}

// perFeedLimit caps how many articles are taken from any single feed,
// before merging; perTagLimit caps the merged, deduplicated total for a
// tag, to bound both the LLM prompt size and the "show everything" list.
const (
	perFeedLimit = 6
	perTagLimit  = 20
)

func googleNewsSearchURL(tag string) string {
	q := url.QueryEscape(strings.TrimSpace(tag) + " when:1d")
	return "https://news.google.com/rss/search?q=" + q + "&hl=en-US&gl=US&ceid=US:en"
}

// --- RSS/Atom parsing -------------------------------------------------

type rssFeed struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Description string `xml:"description"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
	Source      struct {
		Value string `xml:",chardata"`
	} `xml:"source"`
}

var htmlTagRE = regexp.MustCompile(`<[^>]*>`)

func cleanText(s string) string {
	s = htmlTagRE.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

var pubDateFormats = []string{
	time.RFC1123Z,
	time.RFC1123,
	time.RFC3339,
	"2006-01-02T15:04:05Z07:00",
}

func parsePubDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, f := range pubDateFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func fetchFeed(ctx context.Context, client *http.Client, feedURL, tag string) ([]Article, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "short-report/1.0 (+local personal news aggregator)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feed %s returned status %d", feedURL, resp.StatusCode)
	}

	var feed rssFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("parsing feed %s: %w", feedURL, err)
	}

	cutoff := time.Now().Add(-48 * time.Hour)
	articles := make([]Article, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		published := parsePubDate(it.PubDate)
		if !published.IsZero() && published.Before(cutoff) {
			continue // stale item, skip
		}
		articles = append(articles, Article{
			Tag:         tag,
			Title:       cleanText(it.Title),
			Description: cleanText(it.Description),
			Source:      strings.TrimSpace(it.Source.Value),
			Link:        strings.TrimSpace(it.Link),
			Published:   published,
		})
		if len(articles) >= perFeedLimit {
			break
		}
	}
	return articles, nil
}

// FetchTag retrieves articles for a single tag from every curated feed
// for that tag plus a live search, all in parallel, then merges them by
// unique link (independent coverage of the same event by different
// outlets is exactly the diversity we want, so only exact-duplicate
// links are dropped) and returns the most recent perTagLimit.
func FetchTag(ctx context.Context, client *http.Client, tag string) ([]Article, error) {
	key := strings.ToLower(strings.TrimSpace(tag))
	if key == "" {
		return nil, fmt.Errorf("empty tag")
	}

	urls := append([]string{}, curatedFeeds[key]...)
	urls = append(urls, googleNewsSearchURL(tag))

	type result struct {
		articles []Article
		err      error
	}
	results := make([]result, len(urls))
	var wg sync.WaitGroup
	for i, feedURL := range urls {
		wg.Add(1)
		go func(i int, feedURL string) {
			defer wg.Done()
			articles, err := fetchFeed(ctx, client, feedURL, tag)
			results[i] = result{articles: articles, err: err}
		}(i, feedURL)
	}
	wg.Wait()

	seenLinks := make(map[string]bool)
	var merged []Article
	var lastErr error
	for _, r := range results {
		if r.err != nil {
			lastErr = r.err
			continue
		}
		for _, a := range r.articles {
			linkKey := strings.ToLower(a.Link)
			if linkKey == "" || seenLinks[linkKey] {
				continue
			}
			seenLinks[linkKey] = true
			merged = append(merged, a)
		}
	}

	if len(merged) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("no articles found for %q", tag)
	}

	sort.Slice(merged, func(i, j int) bool { return merged[i].Published.After(merged[j].Published) })
	if len(merged) > perTagLimit {
		merged = merged[:perTagLimit]
	}
	return merged, nil
}

// --- Cache --------------------------------------------------------------

type cacheEntry struct {
	articles []Article
	fetched  time.Time
	err      error
}

// Cache holds recently-fetched articles per tag so that regenerating a
// summary (e.g. on every slider move) doesn't hammer upstream feeds.
type Cache struct {
	mu     sync.RWMutex
	ttl    time.Duration
	data   map[string]cacheEntry
	client *http.Client
}

// NewCache creates a tag-keyed article cache with the given freshness
// window.
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		ttl:    ttl,
		data:   make(map[string]cacheEntry),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Cache) getTag(ctx context.Context, tag string) ([]Article, error) {
	key := strings.ToLower(strings.TrimSpace(tag))

	c.mu.RLock()
	entry, ok := c.data[key]
	c.mu.RUnlock()
	if ok && time.Since(entry.fetched) < c.ttl {
		return entry.articles, entry.err
	}

	articles, err := FetchTag(ctx, c.client, tag)
	if err != nil && ok {
		// Serve stale data rather than nothing if a refresh fails.
		return entry.articles, nil
	}

	c.mu.Lock()
	c.data[key] = cacheEntry{articles: articles, fetched: time.Now(), err: err}
	c.mu.Unlock()

	return articles, err
}

// Get fetches (or reuses cached) articles for every tag in parallel and
// returns the combined, recency-sorted list along with any tags that
// could not be fetched at all.
func (c *Cache) Get(ctx context.Context, tags []string) ([]Article, []string) {
	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		all    []Article
		failed []string
	)

	for _, tag := range tags {
		tag := tag
		wg.Add(1)
		go func() {
			defer wg.Done()
			articles, err := c.getTag(ctx, tag)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && len(articles) == 0 {
				failed = append(failed, tag)
				return
			}
			all = append(all, articles...)
		}()
	}
	wg.Wait()

	sort.Slice(all, func(i, j int) bool { return all[i].Published.After(all[j].Published) })
	return all, failed
}
