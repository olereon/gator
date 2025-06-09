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
- `gator agg <time_interval>` - Start continuous feed aggregation (e.g., `gator agg 30s`)
- `gator browse` - View the latest posts from feeds you follow

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

3. Start aggregating feeds:
   ```bash
   gator agg 1m
   ```

4. In another terminal, browse the latest posts:
   ```bash
   gator browse
   ```

## Architecture

- **Language**: Go 1.24.3
- **Database**: PostgreSQL with SQLC for type-safe queries
- **Configuration**: JSON-based configuration stored at `~/.gatorconfig.json`
- **RSS Parsing**: Custom RSS parser for fetching and parsing feed content

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