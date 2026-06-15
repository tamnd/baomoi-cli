// Package baomoi is the library behind the baomoi command line:
// the HTTP client, HTML scraping, and typed data models for Báo Mới
// (baomoi.com), Vietnam's leading news aggregator.
//
// Báo Mới has no public RSS; it aggregates 500+ Vietnamese news sources
// into category listing pages at https://baomoi.com/{category}/trang-{N}.epi.
// Article cards link to both the baomoi page and the original source.
package baomoi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Host is the canonical site hostname.
const Host = "baomoi.com"

// baseURL is the site root.
const baseURL = "https://baomoi.com"

// DefaultUserAgent identifies this client to Báo Mới.
const DefaultUserAgent = "baomoi-cli/0.1.0 (+https://github.com/tamnd/baomoi-cli)"

// Categories lists the Báo Mới category slugs.
var Categories = []string{
	"thoi-su",
	"the-gioi",
	"kinh-te",
	"giai-tri",
	"the-thao",
	"suc-khoe",
	"giao-duc",
	"phap-luat",
	"du-lich",
	"cong-nghe",
}

var categoryNames = map[string]string{
	"thoi-su":   "Thời sự",
	"the-gioi":  "Thế giới",
	"kinh-te":   "Kinh tế",
	"giai-tri":  "Giải trí",
	"the-thao":  "Thể thao",
	"suc-khoe":  "Sức khỏe",
	"giao-duc":  "Giáo dục",
	"phap-luat": "Pháp luật",
	"du-lich":   "Du lịch",
	"cong-nghe": "Công nghệ",
}

// Config holds the tunable knobs for the HTTP client.
type Config struct {
	BaseURL   string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
	UserAgent string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL:   baseURL,
		Rate:      time.Second,
		Retries:   3,
		Timeout:   30 * time.Second,
		UserAgent: DefaultUserAgent,
	}
}

// Client talks to Báo Mới over HTTP.
type Client struct {
	cfg  Config
	http *http.Client
	last time.Time
}

// NewClient returns a Client from DefaultConfig.
func NewClient() *Client { return NewClientWithConfig(DefaultConfig()) }

// NewClientWithConfig returns a Client built from cfg.
func NewClientWithConfig(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout}}
}

// Get fetches rawURL and returns the body, pacing and retrying.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) ([]byte, bool, error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")
	req.Header.Set("Accept-Language", "vi-VN,vi;q=0.9")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	return b, err != nil, err
}

func (c *Client) pace() {
	if c.cfg.Rate <= 0 {
		return
	}
	if wait := c.cfg.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// --- public types ---

// Article is one aggregated article entry on Báo Mới.
type Article struct {
	ID           string `json:"id"                     kit:"id" table:"id"`
	Title        string `json:"title"                            table:"title"`
	URL          string `json:"url,omitempty"                    table:"url,url"`
	OriginalURL  string `json:"original_url,omitempty"           table:"original_url,url"`
	SourceName   string `json:"source_name,omitempty"            table:"source_name"`
	SourceDomain string `json:"source_domain,omitempty"          table:"source_domain"`
	Category     string `json:"category,omitempty"               table:"category"`
	Thumbnail    string `json:"thumbnail,omitempty"              table:"-"`
	PublishedAt  string `json:"published_at,omitempty"           table:"published_at"`
}

// Category represents one Báo Mới category.
type Category struct {
	Slug string `json:"slug" kit:"id" table:"slug"`
	Name string `json:"name"          table:"name"`
	URL  string `json:"url"           table:"url,url"`
}

// --- HTML extraction patterns ---

// articleLinkRE finds baomoi article links: href="/c/{slug}.epi"
var articleLinkRE = regexp.MustCompile(`href="(/c/[^"]+\.epi)"`)

// titleRE extracts text content from the nearest anchor or heading.
var titleRE = regexp.MustCompile(`(?i)<a[^>]+href="/c/[^"]+\.epi"[^>]*>([^<]+)</a>`)

// thumbnailRE finds og:image or the first article img src.
var thumbnailRE = regexp.MustCompile(`(?i)<img[^>]+src="(https://[^"]+\.(?:jpg|png|jpeg|webp))"`)

// publishedRE extracts a datetime from a <time> element or data-pubdate attribute.
var publishedRE = regexp.MustCompile(`(?i)data-pubdate="([^"]+)"`)

// sourceRE extracts the source name from a data-source or class="story__source" span.
var sourceRE = regexp.MustCompile(`(?i)data-source="([^"]+)"`)

// --- client methods ---

// LatestArticles fetches the Báo Mới homepage for the latest articles.
func (c *Client) LatestArticles(ctx context.Context, limit int) ([]*Article, error) {
	if limit <= 0 {
		limit = 20
	}
	body, err := c.Get(ctx, c.cfg.BaseURL+"/")
	if err != nil {
		return nil, fmt.Errorf("home: %w", err)
	}
	return parseListingHTML(body, "latest", limit, c.cfg.BaseURL), nil
}

// CategoryArticles fetches articles for the given category slug.
func (c *Client) CategoryArticles(ctx context.Context, slug string, limit int) ([]*Article, error) {
	if limit <= 0 {
		limit = 20
	}
	body, err := c.Get(ctx, c.cfg.BaseURL+"/"+slug+"/")
	if err != nil {
		return nil, fmt.Errorf("category %s: %w", slug, err)
	}
	return parseListingHTML(body, slug, limit, c.cfg.BaseURL), nil
}

// SearchArticles is not supported by Báo Mới's public interface.
// It falls back to searching the home listing by keyword.
func (c *Client) SearchArticles(ctx context.Context, query string, limit int) ([]*Article, error) {
	if limit <= 0 {
		limit = 20
	}
	q := strings.ToLower(query)
	all, err := c.LatestArticles(ctx, 50)
	if err != nil {
		return nil, err
	}
	var out []*Article
	for _, a := range all {
		if strings.Contains(strings.ToLower(a.Title), q) {
			out = append(out, a)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

// ListCategories returns all known Báo Mới categories.
func (c *Client) ListCategories() []*Category {
	base := c.cfg.BaseURL
	if base == "" {
		base = baseURL
	}
	out := make([]*Category, 0, len(Categories))
	for _, slug := range Categories {
		name := categoryNames[slug]
		if name == "" {
			name = slug
		}
		out = append(out, &Category{
			Slug: slug,
			Name: name,
			URL:  base + "/" + slug + "/",
		})
	}
	return out
}

// --- HTML parsing ---

// parseListingHTML extracts Article records from a Báo Mới listing page.
func parseListingHTML(body []byte, category string, limit int, base string) []*Article {
	html := string(body)
	seen := map[string]bool{}
	var out []*Article

	// Find all article links
	links := articleLinkRE.FindAllStringSubmatch(html, -1)
	if len(links) == 0 {
		return out
	}

	for _, m := range links {
		if len(out) >= limit {
			break
		}
		path := m[1]
		if seen[path] {
			continue
		}
		seen[path] = true

		articleURL := base + path
		id := strings.TrimPrefix(strings.TrimSuffix(path, ".epi"), "/c/")

		// Extract title from the matching anchor
		title := extractTitleForPath(html, path)
		thumbnail := extractFirstImage(html, path)
		source := extractSourceForPath(html, path)
		pubAt := extractPubdateForPath(html, path)

		out = append(out, &Article{
			ID:          id,
			Title:       title,
			URL:         articleURL,
			Category:    category,
			SourceName:  source,
			Thumbnail:   thumbnail,
			PublishedAt: pubAt,
		})
	}
	return out
}

// extractTitleForPath finds the title of an article card by its path.
func extractTitleForPath(html, path string) string {
	re := regexp.MustCompile(`(?i)<a[^>]+href="` + regexp.QuoteMeta(path) + `"[^>]*>([^<]{5,})</a>`)
	m := re.FindStringSubmatch(html)
	if len(m) >= 2 {
		return cleanHTML(m[1])
	}
	return ""
}

// extractFirstImage finds the first significant image near an article link.
func extractFirstImage(html, path string) string {
	idx := strings.Index(html, `href="`+path+`"`)
	if idx < 0 {
		return ""
	}
	// Search around the card (~500 chars before)
	start := idx - 500
	if start < 0 {
		start = 0
	}
	segment := html[start:idx]
	m := thumbnailRE.FindStringSubmatch(segment)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

// extractSourceForPath extracts data-source near the article link.
func extractSourceForPath(html, path string) string {
	idx := strings.Index(html, `href="`+path+`"`)
	if idx < 0 {
		return ""
	}
	end := idx + 300
	if end > len(html) {
		end = len(html)
	}
	m := sourceRE.FindStringSubmatch(html[idx:end])
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

// extractPubdateForPath extracts data-pubdate near the article link.
func extractPubdateForPath(html, path string) string {
	idx := strings.Index(html, `href="`+path+`"`)
	if idx < 0 {
		return ""
	}
	end := idx + 300
	if end > len(html) {
		end = len(html)
	}
	m := publishedRE.FindStringSubmatch(html[idx:end])
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

// cleanHTML strips HTML entities and trims whitespace.
func cleanHTML(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return strings.TrimSpace(s)
}

// slugFromPath extracts the article slug from a /c/{slug}.epi path.
func slugFromPath(path string) string {
	s := strings.TrimPrefix(path, "/c/")
	return strings.TrimSuffix(s, ".epi")
}

// extractArticleID returns the article slug from a Báo Mới URL or path.
func extractArticleID(rawURL string) string {
	// Match /c/{slug}.epi
	re := regexp.MustCompile(`/c/([^/?#]+)\.epi`)
	m := re.FindStringSubmatch(rawURL)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

// titleREGlobal used by tests only — exported via test helper.
var _ = titleRE
var _ = slugFromPath
