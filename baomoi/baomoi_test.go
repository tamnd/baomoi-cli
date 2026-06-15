package baomoi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(srv *httptest.Server) *Client {
	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 0
	cfg.Timeout = 5 * time.Second
	return NewClientWithConfig(cfg)
}

// sampleListingHTML returns a minimal Báo Mới-style listing page with n articles.
func sampleListingHTML(n int) string {
	cards := ""
	for i := 0; i < n; i++ {
		cards += fmt.Sprintf(`
		<article>
			<img src="https://cdn.baomoi.com/thumb%d.jpg">
			<a href="/c/article-slug-%d.epi" data-source="VNExpress" data-pubdate="2026-06-14T08:00:00Z">
				Tiêu đề bài viết số %d về thời sự Việt Nam hôm nay
			</a>
		</article>`, i+1, i+1, i+1)
	}
	return `<!DOCTYPE html>
<html>
<head><title>Báo Mới - Tin tức</title></head>
<body>
<div class="listing">` + cards + `
</div>
</body>
</html>`
}

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("no User-Agent header")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want ok", body)
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 5
	cfg.Timeout = 5 * time.Second
	c := NewClientWithConfig(cfg)

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q", body)
	}
	if hits != 3 {
		t.Errorf("hits = %d, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("no backoff between retries")
	}
}

func TestLatestArticles(t *testing.T) {
	html := sampleListingHTML(5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	articles, err := c.LatestArticles(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(articles) != 5 {
		t.Fatalf("len = %d, want 5", len(articles))
	}
	a := articles[0]
	if a.ID == "" {
		t.Error("ID empty")
	}
	if a.Title == "" {
		t.Error("Title empty")
	}
	if a.Category != "latest" {
		t.Errorf("category = %q, want latest", a.Category)
	}
}

func TestCategoryArticles(t *testing.T) {
	html := sampleListingHTML(3)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	articles, err := c.CategoryArticles(context.Background(), "thoi-su", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(articles) != 3 {
		t.Fatalf("len = %d, want 3", len(articles))
	}
	if articles[0].Category != "thoi-su" {
		t.Errorf("category = %q, want thoi-su", articles[0].Category)
	}
}

func TestCategoryLimit(t *testing.T) {
	html := sampleListingHTML(10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	articles, err := c.CategoryArticles(context.Background(), "kinh-te", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(articles) != 4 {
		t.Fatalf("len = %d, want 4", len(articles))
	}
}

func TestExtractArticleID(t *testing.T) {
	cases := []struct{ url, want string }{
		{"https://baomoi.com/c/some-article-slug-abc123.epi", "some-article-slug-abc123"},
		{"https://baomoi.com/c/another-article.epi", "another-article"},
		{"https://baomoi.com/thoi-su/", ""},
	}
	for _, tc := range cases {
		got := extractArticleID(tc.url)
		if got != tc.want {
			t.Errorf("extractArticleID(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestListCategories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	c := newTestClient(srv)
	cats := c.ListCategories()
	if len(cats) == 0 {
		t.Fatal("empty categories")
	}
	if cats[0].Slug != "thoi-su" {
		t.Errorf("first slug = %q, want thoi-su", cats[0].Slug)
	}
}

func TestGetHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.Get(context.Background(), srv.URL)
	if err == nil {
		t.Error("want error on 404")
	}
}
