package baomoi

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

func init() { kit.Register(Domain{}) }

// Domain is the Báo Mới driver.
type Domain struct{}

func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "baomoi",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "baomoi",
			Short:  "Read public Báo Mới (baomoi.com) aggregated news articles.",
			Long: `Read public Báo Mới (baomoi.com) aggregated news articles.

baomoi scrapes baomoi.com category listing pages — no API key, no browser required.
Báo Mới aggregates 500+ Vietnamese news sources; each article record includes
the original source name and URL. Returns clean JSON records ready for jq,
sqlite-utils, and shell pipelines.`,
			Site: Host,
			Repo: "https://github.com/tamnd/baomoi-cli",
		},
	}
}

func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{Name: "article", Group: "read", Single: true,
		URIType: "article", Resolver: true,
		Summary: "Resolve a Báo Mới article slug to its URL",
		Args:    []kit.Arg{{Name: "id", Help: "article slug (from /c/{slug}.epi)"}}}, getArticle)

	kit.Handle(app, kit.OpMeta{Name: "latest", Group: "read", List: true,
		URIType: "article",
		Summary: "Fetch the latest Báo Mới aggregated articles"}, getLatest)

	kit.Handle(app, kit.OpMeta{Name: "category", Group: "read", List: true,
		URIType: "article",
		Summary: "Fetch articles for a category (e.g. thoi-su, kinh-te)",
		Args:    []kit.Arg{{Name: "slug", Help: "category slug"}}}, getCategoryArticles)

	kit.Handle(app, kit.OpMeta{Name: "search", Group: "read", List: true,
		URIType: "article",
		Summary: "Search Báo Mới articles by keyword (scans home listing)",
		Args:    []kit.Arg{{Name: "query", Help: "search keyword"}}}, searchArticles)

	kit.Handle(app, kit.OpMeta{Name: "categories", Group: "read", List: true,
		Summary: "List all Báo Mới categories"}, listCategories)
}

func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := DefaultConfig()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.Timeout = cfg.Timeout
	}
	return NewClientWithConfig(c), nil
}

type articleInput struct {
	ID     string  `kit:"arg"   help:"article slug"`
	Client *Client `kit:"inject"`
}

type latestInput struct {
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

type categoryInput struct {
	Slug   string  `kit:"arg"          help:"category slug"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

type searchInput struct {
	Query  string  `kit:"arg"          help:"search keyword"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

type categoriesInput struct {
	Client *Client `kit:"inject"`
}

func getArticle(_ context.Context, in articleInput, emit func(*Article) error) error {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return errs.Usage("article slug is required")
	}
	return emit(&Article{
		ID:  id,
		URL: baseURL + "/c/" + id + ".epi",
	})
}

func getLatest(ctx context.Context, in latestInput, emit func(*Article) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	articles, err := in.Client.LatestArticles(ctx, limit)
	if err != nil {
		return err
	}
	for _, a := range articles {
		if err := emit(a); err != nil {
			return err
		}
	}
	return nil
}

func getCategoryArticles(ctx context.Context, in categoryInput, emit func(*Article) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	articles, err := in.Client.CategoryArticles(ctx, in.Slug, limit)
	if err != nil {
		return err
	}
	for _, a := range articles {
		if err := emit(a); err != nil {
			return err
		}
	}
	return nil
}

func searchArticles(ctx context.Context, in searchInput, emit func(*Article) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	articles, err := in.Client.SearchArticles(ctx, in.Query, limit)
	if err != nil {
		return err
	}
	for _, a := range articles {
		if err := emit(a); err != nil {
			return err
		}
	}
	return nil
}

func listCategories(_ context.Context, in categoriesInput, emit func(*Category) error) error {
	for _, cat := range in.Client.ListCategories() {
		if err := emit(cat); err != nil {
			return err
		}
	}
	return nil
}

// Classify turns a Báo Mới URL or article slug into (type, id).
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errs.Usage("empty Báo Mới reference")
	}
	if strings.Contains(input, "baomoi.com/c/") || strings.HasSuffix(input, ".epi") {
		id = extractArticleID(input)
		if id == "" {
			// bare slug like "some-article-slug"
			id = strings.TrimSuffix(input, ".epi")
		}
		return "article", id, nil
	}
	for _, slug := range Categories {
		if strings.EqualFold(input, slug) {
			return "category", slug, nil
		}
	}
	return "", "", errs.Usage("unrecognized Báo Mới reference: %q", input)
}

// Locate returns the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "article":
		return baseURL + "/c/" + id + ".epi", nil
	case "category":
		return baseURL + "/" + id + "/", nil
	default:
		return "", errs.Usage("baomoi has no resource type %q", uriType)
	}
}
