package ingest

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/hpwn/EloraChat/src/backend/routes"
)

// Ingestor abstracts a chat ingestion implementation (Start may spawn goroutines).
type Ingestor interface {
	Start(ctx context.Context, urls []string) error
	Name() string
}

// ChatDownloaderIngestor delegates to the existing routes.StartChatFetch.
type ChatDownloaderIngestor struct{}

func (c *ChatDownloaderIngestor) Start(ctx context.Context, urls []string) error {
	if len(urls) == 0 {
		log.Printf("ingest[%s]: no URLs provided; skipping", c.Name())
		return nil
	}
	routes.StartChatFetch(urls)
	return nil
}

func (c *ChatDownloaderIngestor) Name() string { return "chatdownloader" }

// GnastyIngestor is a stub placeholder for future work.
type GnastyIngestor struct{}

func (g *GnastyIngestor) Start(ctx context.Context, urls []string) error {
	log.Printf("ingest[%s]: stub enabled with %d URLs (%s); no-op for now",
		g.Name(), len(urls), strings.Join(urls, ", "))
	return nil
}

func (g *GnastyIngestor) Name() string { return "gnasty" }

// New returns an Ingestor by driver name.
func New(driver string) (Ingestor, error) {
	drv := strings.ToLower(strings.TrimSpace(driver))
	switch drv {
	case "", "chatdownloader", "chat_downloader":
		return &ChatDownloaderIngestor{}, nil
	case "gnasty", "gnastychat", "gnasty-chat":
		return &GnastyIngestor{}, nil
	default:
		return nil, fmt.Errorf("unknown ELORA_INGEST_DRIVER=%q", driver)
	}
}
