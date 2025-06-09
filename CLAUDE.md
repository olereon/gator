# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Gator is a CLI-based RSS aggregator built with Go that uses PostgreSQL as its database backend. The application allows users to aggregate and manage RSS feeds from the command line.

## Architecture

- **Language**: Go 1.24.3 (Note: This version should be corrected to a valid Go version like 1.21, 1.22, or 1.23)
- **Database**: PostgreSQL
- **Configuration**: JSON-based configuration stored at `~/.gatorconfig.json`
- **Project Structure**: 
  - `internal/` - Private packages following Go best practices
  - `internal/config/` - Configuration handling code

## Common Development Commands

### Database Setup
```bash
# Ensure PostgreSQL is running and accessible
# Update the db_url in ~/.gatorconfig.json with your PostgreSQL connection string
```

### Build Commands
```bash
# Build the application
go build -o gator .

# Run the application
./gator

# Install globally
go install
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests in verbose mode
go test -v ./...
```

### Development Workflow
```bash
# Format code
go fmt ./...

# Run linter (install: go install golang.org/x/tools/cmd/goimports@latest)
goimports -w .

# Check for common mistakes
go vet ./...

# Download dependencies
go mod download

# Tidy dependencies
go mod tidy
```

## Key Implementation Notes

1. **Configuration Management**: The application reads its configuration from `~/.gatorconfig.json`. The `db_url` field contains the PostgreSQL connection string.

2. **Database Interactions**: Since this is an RSS aggregator using PostgreSQL, implement:
   - Feed management (add, remove, list feeds)
   - Article storage and retrieval
   - User preferences and subscriptions

3. **CLI Structure**: As a CLI tool, consider using a command structure like:
   - `gator add <feed-url>` - Add a new RSS feed
   - `gator list` - List all subscribed feeds
   - `gator fetch` - Fetch latest articles from all feeds
   - `gator read` - Display unread articles

4. **Internal Package Structure**: Keep database models, RSS parsing logic, and configuration handling within the `internal/` directory to prevent external imports.