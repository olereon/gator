# Gator

Gator is a command-line RSS aggregator built with Go that uses PostgreSQL as its database backend. It allows users to aggregate and manage RSS feeds from the command line.

## Prerequisites

Before running Gator, you'll need to have the following installed:

- **Go** (version 1.21 or later)
- **PostgreSQL** (running and accessible)

## Installation

Install Gator using Go's built-in package manager:

```bash
go install github.com/olereon/gator@latest
```

## Configuration

Create a configuration file at `~/.gatorconfig.json` with your PostgreSQL connection details:

```json
{
  "db_url": "postgres://username:password@localhost/gator?sslmode=disable",
  "current_user_name": ""
}
```

Replace `username`, `password`, and `localhost` with your PostgreSQL credentials and host.

## Database Setup

Before using Gator, you'll need to run the database migrations. Navigate to the project directory and run:

```bash
cd sql/schema
goose postgres "your_postgres_connection_string" up
```

Replace `your_postgres_connection_string` with the same URL you used in your config file.

## Usage

Gator provides several commands to manage RSS feeds and users:

### User Management
- `gator register <username>` - Create a new user and set as current
- `gator login <username>` - Switch to an existing user
- `gator users` - List all users (current user marked with *)
- `gator reset` - Clear all data from the database

### Feed Management
- `gator addfeed <name> <url>` - Add a new RSS feed (automatically follows it)
- `gator feeds` - List all feeds with their creators
- `gator follow <url>` - Follow an existing feed
- `gator following` - List feeds you're following
- `gator unfollow <url>` - Unfollow a feed

### Content Aggregation
- `gator agg <time_interval> [concurrency]` - Start continuous feed aggregation (e.g., `gator agg 30s 10`)
- `gator browse [options]` - View posts from feeds you follow with advanced options:
  - `--limit=N` - Number of posts to show (default: 10)
  - `--offset=N` - Number of posts to skip for pagination (default: 0)
  - `--sort=OPTION` - Sort by: published_desc, published, title, title_desc, feed, feed_desc
  - `--feed=NAME` - Filter by feed name (partial match)
  - `--help` - Show help for browse command
- `gator search <query>` - Search posts by title, description, or feed name
- `gator tui` - Interactive terminal interface for browsing and opening posts

### Bookmarks
- `gator bookmark <post_url>` - Bookmark a post for later reading
- `gator unbookmark <post_url>` - Remove a bookmark
- `gator bookmarks [limit]` - View your bookmarked posts

## Example Workflow

1. Register a new user:
   ```bash
   gator register john
   ```

2. Add some RSS feeds:
   ```bash
   gator addfeed "TechCrunch" "https://techcrunch.com/feed/"
   gator addfeed "Hacker News" "https://hnrss.org/frontpage"
   ```

3. Start aggregating feeds with concurrency:
   ```bash
   gator agg 1m 5
   ```

4. Browse and interact with posts:
   ```bash
   # Browse latest posts with pagination
   gator browse --limit=20 --sort=published_desc
   
   # Search for specific content
   gator search "golang programming"
   
   # Use interactive TUI
   gator tui
   
   # Bookmark interesting posts
   gator bookmark "https://example.com/article"
   
   # View your bookmarks
   gator bookmarks
   ```

## Features

- **Multi-user Support**: Register and manage multiple users
- **RSS Feed Management**: Add, follow, and unfollow RSS feeds
- **Concurrent Aggregation**: Fetch multiple feeds simultaneously for faster updates
- **Advanced Search**: Full-text search across post titles, descriptions, and feed names
- **Bookmarking**: Save interesting posts for later reading
- **Interactive TUI**: Terminal user interface for easy post browsing and opening
- **Flexible Browsing**: Sort, filter, and paginate through posts
- **Cross-platform**: Works on Windows, macOS, and Linux

## Architecture

- **Language**: Go 1.24.3
- **Database**: PostgreSQL with SQLC for type-safe queries
- **Configuration**: JSON-based configuration stored at `~/.gatorconfig.json`
- **RSS Parsing**: Custom RSS parser for fetching and parsing feed content
- **Concurrency**: Goroutines for parallel feed fetching

## Project Structure

```
├── internal/
│   ├── config/     # Configuration handling
│   ├── database/   # Generated SQLC code and models
│   └── rss/        # RSS parsing functionality
├── sql/
│   ├── queries/    # SQL queries for SQLC
│   └── schema/     # Database migrations
├── main.go         # Main application and command handlers
└── README.md       # This file
```