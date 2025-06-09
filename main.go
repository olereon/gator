package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/olereon/Gator/internal/config"
	"github.com/olereon/Gator/internal/database"
	"github.com/olereon/Gator/internal/rss"
)

type state struct {
	db  *database.Queries
	cfg *config.Config
}

type command struct {
	name string
	args []string
}

type commands struct {
	handlers map[string]func(*state, command) error
}

func (c *commands) register(name string, f func(*state, command) error) {
	c.handlers[name] = f
}

func (c *commands) run(s *state, cmd command) error {
	handler, exists := c.handlers[cmd.name]
	if !exists {
		return fmt.Errorf("unknown command: %s", cmd.name)
	}
	return handler(s, cmd)
}

func middlewareLoggedIn(handler func(s *state, cmd command, user database.User) error) func(*state, command) error {
	return func(s *state, cmd command) error {
		user, err := s.db.GetUserByName(context.Background(), s.cfg.CurrentUserName)
		if err != nil {
			return fmt.Errorf("couldn't get user: %w", err)
		}
		return handler(s, cmd, user)
	}
}

func handlerLogin(s *state, cmd command) error {
	if len(cmd.args) == 0 {
		return errors.New("username is required")
	}

	username := cmd.args[0]
	
	// Check if user exists in database
	_, err := s.db.GetUserByName(context.Background(), username)
	if err != nil {
		return fmt.Errorf("user %s doesn't exist", username)
	}

	// Set current user in config
	err = s.cfg.SetUser(username)
	if err != nil {
		return fmt.Errorf("couldn't set user: %w", err)
	}

	fmt.Printf("User has been set to: %s\n", username)
	return nil
}

func handlerRegister(s *state, cmd command) error {
	if len(cmd.args) == 0 {
		return errors.New("username is required")
	}

	username := cmd.args[0]

	// Create new user in database
	user, err := s.db.CreateUser(context.Background(), database.CreateUserParams{
		ID:        uuid.New(),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Name:      username,
	})
	if err != nil {
		// Check if it's a unique constraint violation (user already exists)
		if err.Error() == `pq: duplicate key value violates unique constraint "users_name_key"` {
			return fmt.Errorf("user %s already exists", username)
		}
		return fmt.Errorf("couldn't create user: %w", err)
	}

	// Set current user in config
	err = s.cfg.SetUser(username)
	if err != nil {
		return fmt.Errorf("couldn't set current user: %w", err)
	}

	fmt.Printf("User %s was created successfully!\n", username)
	fmt.Printf("User data: ID=%s, Name=%s, CreatedAt=%s\n", 
		user.ID, user.Name, user.CreatedAt.Format(time.RFC3339))

	return nil
}

func handlerReset(s *state, cmd command) error {
	// Delete all users from the database
	err := s.db.DeleteAllUsers(context.Background())
	if err != nil {
		return fmt.Errorf("couldn't reset database: %w", err)
	}

	fmt.Println("Database has been reset!")
	return nil
}

func handlerUsers(s *state, cmd command) error {
	// Get all users from the database
	users, err := s.db.GetUsers(context.Background())
	if err != nil {
		return fmt.Errorf("couldn't get users: %w", err)
	}

	// Get current user from config
	currentUser := s.cfg.CurrentUserName

	// Print all users
	for _, user := range users {
		if user.Name == currentUser {
			fmt.Printf("* %s (current)\n", user.Name)
		} else {
			fmt.Printf("* %s\n", user.Name)
		}
	}

	return nil
}

func scrapeFeed(s *state, feed database.Feed, wg *sync.WaitGroup) {
	defer wg.Done()

	// Mark it as fetched
	err := s.db.MarkFeedFetched(context.Background(), feed.ID)
	if err != nil {
		fmt.Printf("Error marking feed %s as fetched: %v\n", feed.Name, err)
		return
	}

	// Fetch the feed
	rssFeed, err := rss.FetchFeed(context.Background(), feed.Url)
	if err != nil {
		fmt.Printf("Error fetching feed %s: %v\n", feed.Name, err)
		return
	}

	// Save posts to database
	fmt.Printf("Found %d posts in %s\n", len(rssFeed.Channel.Item), feed.Name)
	for _, item := range rssFeed.Channel.Item {
		// Create post in database
		_, err := s.db.CreatePost(context.Background(), database.CreatePostParams{
			ID:          uuid.New(),
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
			Title:       item.Title,
			Url:         item.Link,
			Description: sql.NullString{String: item.Description, Valid: item.Description != ""},
			PublishedAt: sql.NullTime{Time: item.PubDate, Valid: !item.PubDate.IsZero()},
			FeedID:      feed.ID,
		})
		if err != nil {
			// Ignore duplicate URL errors
			if err.Error() != `pq: duplicate key value violates unique constraint "posts_url_key"` {
				fmt.Printf("Error creating post %s: %v\n", item.Title, err)
			}
		}
	}
}

func scrapeFeeds(s *state, concurrency int) {
	// Get multiple feeds to fetch
	feeds, err := s.db.GetNextFeedsToFetch(context.Background(), int32(concurrency))
	if err != nil {
		fmt.Printf("Error getting feeds: %v\n", err)
		return
	}

	if len(feeds) == 0 {
		fmt.Println("No feeds to fetch")
		return
	}

	fmt.Printf("Fetching %d feeds concurrently\n", len(feeds))

	var wg sync.WaitGroup
	for _, feed := range feeds {
		wg.Add(1)
		go scrapeFeed(s, feed, &wg)
	}
	wg.Wait()
}

func handlerAgg(s *state, cmd command) error {
	if len(cmd.args) == 0 {
		return errors.New("time_between_reqs is required")
	}

	timeBetweenRequests, err := time.ParseDuration(cmd.args[0])
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}

	// Default concurrency
	concurrency := 5

	// Parse optional concurrency argument
	if len(cmd.args) > 1 {
		if c, err := strconv.Atoi(cmd.args[1]); err == nil && c > 0 {
			concurrency = c
		} else {
			return fmt.Errorf("invalid concurrency value: %s", cmd.args[1])
		}
	}

	fmt.Printf("Collecting feeds every %s with concurrency %d\n", timeBetweenRequests, concurrency)

	ticker := time.NewTicker(timeBetweenRequests)
	for ; ; <-ticker.C {
		scrapeFeeds(s, concurrency)
	}
}

func handlerAddFeed(s *state, cmd command, user database.User) error {
	if len(cmd.args) < 2 {
		return errors.New("name and url are required")
	}

	name := cmd.args[0]
	url := cmd.args[1]

	// Create the feed
	feed, err := s.db.CreateFeed(context.Background(), database.CreateFeedParams{
		ID:        uuid.New(),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Name:      name,
		Url:       url,
		UserID:    user.ID,
	})
	if err != nil {
		return fmt.Errorf("couldn't create feed: %w", err)
	}

	// Automatically follow the feed
	feedFollow, err := s.db.CreateFeedFollow(context.Background(), database.CreateFeedFollowParams{
		ID:        uuid.New(),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		UserID:    user.ID,
		FeedID:    feed.ID,
	})
	if err != nil {
		return fmt.Errorf("couldn't follow feed: %w", err)
	}

	fmt.Printf("Feed %s created successfully!\n", feed.Name)
	fmt.Printf("%s is now following %s\n", feedFollow.UserName, feedFollow.FeedName)

	return nil
}

func handlerFeeds(s *state, cmd command) error {
	// Get all feeds with user information
	feeds, err := s.db.GetFeedsWithUsers(context.Background())
	if err != nil {
		return fmt.Errorf("couldn't get feeds: %w", err)
	}

	// Print all feeds
	for _, feed := range feeds {
		fmt.Printf("* %s\n", feed.FeedName)
		fmt.Printf("  URL: %s\n", feed.FeedUrl)
		fmt.Printf("  Created by: %s\n", feed.UserName)
		fmt.Println()
	}

	return nil
}

func handlerFollow(s *state, cmd command, user database.User) error {
	if len(cmd.args) == 0 {
		return errors.New("url is required")
	}

	url := cmd.args[0]

	// Get feed by URL
	feed, err := s.db.GetFeedByURL(context.Background(), url)
	if err != nil {
		return fmt.Errorf("couldn't find feed: %w", err)
	}

	// Create feed follow
	feedFollow, err := s.db.CreateFeedFollow(context.Background(), database.CreateFeedFollowParams{
		ID:        uuid.New(),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		UserID:    user.ID,
		FeedID:    feed.ID,
	})
	if err != nil {
		if err.Error() == `pq: duplicate key value violates unique constraint "feed_follows_user_id_feed_id_key"` {
			return fmt.Errorf("you are already following this feed")
		}
		return fmt.Errorf("couldn't create feed follow: %w", err)
	}

	fmt.Printf("%s is now following %s\n", feedFollow.UserName, feedFollow.FeedName)

	return nil
}

func handlerFollowing(s *state, cmd command, user database.User) error {
	// Get feed follows for user
	feedFollows, err := s.db.GetFeedFollowsForUser(context.Background(), user.ID)
	if err != nil {
		return fmt.Errorf("couldn't get feed follows: %w", err)
	}

	// Print followed feeds
	fmt.Printf("Feeds followed by %s:\n", user.Name)
	for _, ff := range feedFollows {
		fmt.Printf("* %s\n", ff.FeedName)
	}

	return nil
}

func handlerUnfollow(s *state, cmd command, user database.User) error {
	if len(cmd.args) == 0 {
		return errors.New("url is required")
	}

	url := cmd.args[0]

	// Delete feed follow
	err := s.db.DeleteFeedFollow(context.Background(), database.DeleteFeedFollowParams{
		UserID: user.ID,
		Url:    url,
	})
	if err != nil {
		return fmt.Errorf("couldn't unfollow feed: %w", err)
	}

	fmt.Printf("%s unfollowed %s\n", user.Name, url)

	return nil
}

func handlerBrowse(s *state, cmd command, user database.User) error {
	// Default values
	limit := int32(10)
	offset := int32(0)
	sortBy := "published_desc"
	feedFilter := ""

	// Parse arguments
	for i, arg := range cmd.args {
		if strings.HasPrefix(arg, "--limit=") {
			if l, err := strconv.Atoi(strings.TrimPrefix(arg, "--limit=")); err == nil && l > 0 {
				limit = int32(l)
			}
		} else if strings.HasPrefix(arg, "--offset=") {
			if o, err := strconv.Atoi(strings.TrimPrefix(arg, "--offset=")); err == nil && o >= 0 {
				offset = int32(o)
			}
		} else if strings.HasPrefix(arg, "--sort=") {
			sortBy = strings.TrimPrefix(arg, "--sort=")
		} else if strings.HasPrefix(arg, "--feed=") {
			feedFilter = strings.TrimPrefix(arg, "--feed=")
		} else if arg == "--help" {
			fmt.Println("Usage: gator browse [options]")
			fmt.Println("Options:")
			fmt.Println("  --limit=N        Number of posts to show (default: 10)")
			fmt.Println("  --offset=N       Number of posts to skip (default: 0)")
			fmt.Println("  --sort=OPTION    Sort by: published_desc, published, title, title_desc, feed, feed_desc (default: published_desc)")
			fmt.Println("  --feed=NAME      Filter by feed name (partial match)")
			fmt.Println("  --help           Show this help")
			return nil
		} else if i == 0 {
			// First argument without flag is treated as limit for backward compatibility
			if l, err := strconv.Atoi(arg); err == nil && l > 0 {
				limit = int32(l)
			}
		}
	}

	// Validate sort option
	validSorts := map[string]bool{
		"published_desc": true, "published": true, "title": true,
		"title_desc": true, "feed": true, "feed_desc": true,
	}
	if !validSorts[sortBy] {
		return fmt.Errorf("invalid sort option: %s. Valid options: published_desc, published, title, title_desc, feed, feed_desc", sortBy)
	}

	// Get posts for user with pagination
	posts, err := s.db.GetPostsForUserWithPagination(context.Background(), database.GetPostsForUserWithPaginationParams{
		UserID:  user.ID,
		Column2: feedFilter,
		Column3: sortBy,
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		return fmt.Errorf("couldn't get posts: %w", err)
	}

	if len(posts) == 0 {
		fmt.Println("No posts found.")
		return nil
	}

	// Print posts
	fmt.Printf("Showing %d posts (offset %d, sorted by %s", len(posts), offset, sortBy)
	if feedFilter != "" {
		fmt.Printf(", filtered by feed: %s", feedFilter)
	}
	fmt.Println(")")
	fmt.Println()

	for i, post := range posts {
		fmt.Printf("%d. %s\n", int(offset)+i+1, post.Title)
		if post.Description.Valid && post.Description.String != "" {
			description := post.Description.String
			if len(description) > 150 {
				description = description[:147] + "..."
			}
			fmt.Printf("   %s\n", description)
		}
		fmt.Printf("   Link: %s\n", post.Url)
		fmt.Printf("   Feed: %s\n", post.FeedName)
		if post.PublishedAt.Valid {
			fmt.Printf("   Published: %s\n", post.PublishedAt.Time.Format("Mon, 02 Jan 2006 15:04:05 MST"))
		}
		fmt.Println()
	}

	// Show pagination info
	if len(posts) == int(limit) {
		fmt.Printf("To see more posts, use: gator browse --offset=%d\n", offset+limit)
	}

	return nil
}

func handlerSearch(s *state, cmd command, user database.User) error {
	if len(cmd.args) == 0 {
		return errors.New("search query is required")
	}

	query := strings.Join(cmd.args, " ")
	limit := int32(20)

	// Search for posts
	posts, err := s.db.SearchPostsForUser(context.Background(), database.SearchPostsForUserParams{
		UserID:  user.ID,
		Column2: sql.NullString{String: query, Valid: true},
		Limit:   limit,
	})
	if err != nil {
		return fmt.Errorf("couldn't search posts: %w", err)
	}

	if len(posts) == 0 {
		fmt.Printf("No posts found for query: %s\n", query)
		return nil
	}

	fmt.Printf("Found %d posts matching \"%s\":\n\n", len(posts), query)

	for i, post := range posts {
		fmt.Printf("%d. %s\n", i+1, post.Title)
		if post.Description.Valid && post.Description.String != "" {
			description := post.Description.String
			if len(description) > 150 {
				description = description[:147] + "..."
			}
			fmt.Printf("   %s\n", description)
		}
		fmt.Printf("   Link: %s\n", post.Url)
		fmt.Printf("   Feed: %s\n", post.FeedName)
		if post.PublishedAt.Valid {
			fmt.Printf("   Published: %s\n", post.PublishedAt.Time.Format("Mon, 02 Jan 2006 15:04:05 MST"))
		}
		fmt.Println()
	}

	return nil
}

func handlerBookmark(s *state, cmd command, user database.User) error {
	if len(cmd.args) == 0 {
		return errors.New("post URL is required")
	}

	postURL := cmd.args[0]

	// Find the post by URL
	post, err := s.db.GetPostByURL(context.Background(), postURL)
	if err != nil {
		return fmt.Errorf("couldn't find post: %w", err)
	}

	// Check if already bookmarked
	isBookmarked, err := s.db.IsPostBookmarked(context.Background(), database.IsPostBookmarkedParams{
		UserID: user.ID,
		PostID: post.ID,
	})
	if err != nil {
		return fmt.Errorf("couldn't check bookmark status: %w", err)
	}

	if isBookmarked.IsBookmarked {
		fmt.Println("Post is already bookmarked")
		return nil
	}

	// Create bookmark
	_, err = s.db.CreateBookmark(context.Background(), database.CreateBookmarkParams{
		ID:        uuid.New(),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		UserID:    user.ID,
		PostID:    post.ID,
	})
	if err != nil {
		return fmt.Errorf("couldn't create bookmark: %w", err)
	}

	fmt.Printf("Bookmarked: %s\n", post.Title)
	return nil
}

func handlerUnbookmark(s *state, cmd command, user database.User) error {
	if len(cmd.args) == 0 {
		return errors.New("post URL is required")
	}

	postURL := cmd.args[0]

	// Find the post by URL
	post, err := s.db.GetPostByURL(context.Background(), postURL)
	if err != nil {
		return fmt.Errorf("couldn't find post: %w", err)
	}

	// Delete bookmark
	err = s.db.DeleteBookmark(context.Background(), database.DeleteBookmarkParams{
		UserID: user.ID,
		PostID: post.ID,
	})
	if err != nil {
		return fmt.Errorf("couldn't remove bookmark: %w", err)
	}

	fmt.Printf("Removed bookmark: %s\n", post.Title)
	return nil
}

func handlerBookmarks(s *state, cmd command, user database.User) error {
	limit := int32(20)

	// Parse optional limit argument
	if len(cmd.args) > 0 {
		if l, err := strconv.Atoi(cmd.args[0]); err == nil && l > 0 {
			limit = int32(l)
		}
	}

	// Get bookmarks for user
	bookmarks, err := s.db.GetBookmarksForUser(context.Background(), database.GetBookmarksForUserParams{
		UserID: user.ID,
		Limit:  limit,
	})
	if err != nil {
		return fmt.Errorf("couldn't get bookmarks: %w", err)
	}

	if len(bookmarks) == 0 {
		fmt.Println("No bookmarks found.")
		return nil
	}

	fmt.Printf("Your %d bookmark(s):\n\n", len(bookmarks))

	for i, bookmark := range bookmarks {
		fmt.Printf("%d. %s\n", i+1, bookmark.Title)
		if bookmark.Description.Valid && bookmark.Description.String != "" {
			description := bookmark.Description.String
			if len(description) > 150 {
				description = description[:147] + "..."
			}
			fmt.Printf("   %s\n", description)
		}
		fmt.Printf("   Link: %s\n", bookmark.Url)
		fmt.Printf("   Feed: %s\n", bookmark.FeedName)
		if bookmark.PublishedAt.Valid {
			fmt.Printf("   Published: %s\n", bookmark.PublishedAt.Time.Format("Mon, 02 Jan 2006 15:04:05 MST"))
		}
		fmt.Printf("   Bookmarked: %s\n", bookmark.BookmarkedAt.Format("Mon, 02 Jan 2006 15:04:05 MST"))
		fmt.Println()
	}

	return nil
}

func openURL(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	default: // "linux", "freebsd", "openbsd", "netbsd"
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}

func handlerTUI(s *state, cmd command, user database.User) error {
	limit := int32(10)

	// Get recent posts
	posts, err := s.db.GetPostsForUser(context.Background(), database.GetPostsForUserParams{
		UserID: user.ID,
		Limit:  limit,
	})
	if err != nil {
		return fmt.Errorf("couldn't get posts: %w", err)
	}

	if len(posts) == 0 {
		fmt.Println("No posts found.")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)

	for {
		// Clear screen (works on most terminals)
		fmt.Print("\033[2J\033[H")

		fmt.Println("=== Gator TUI - Latest Posts ===")
		fmt.Println()

		// Display posts
		for i, post := range posts {
			fmt.Printf("%d. %s\n", i+1, post.Title)
			if post.Description.Valid && post.Description.String != "" {
				description := post.Description.String
				if len(description) > 100 {
					description = description[:97] + "..."
				}
				fmt.Printf("   %s\n", description)
			}
			fmt.Printf("   Feed: %s", post.FeedName)
			if post.PublishedAt.Valid {
				fmt.Printf(" | %s", post.PublishedAt.Time.Format("Jan 02"))
			}
			fmt.Println()
		}

		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  1-10    Open post in browser")
		fmt.Println("  r       Refresh posts")
		fmt.Println("  s       Search posts")
		fmt.Println("  b       View bookmarks")
		fmt.Println("  q       Quit")
		fmt.Print("\nEnter command: ")

		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("error reading input: %w", err)
		}

		input = strings.TrimSpace(input)

		switch input {
		case "q":
			fmt.Println("Goodbye!")
			return nil

		case "r":
			// Refresh posts
			posts, err = s.db.GetPostsForUser(context.Background(), database.GetPostsForUserParams{
				UserID: user.ID,
				Limit:  limit,
			})
			if err != nil {
				fmt.Printf("Error refreshing posts: %v\n", err)
				fmt.Print("Press Enter to continue...")
				reader.ReadString('\n')
			}

		case "s":
			fmt.Print("Enter search query: ")
			query, err := reader.ReadString('\n')
			if err != nil {
				fmt.Printf("Error reading query: %v\n", err)
				continue
			}
			query = strings.TrimSpace(query)
			if query == "" {
				continue
			}

			searchResults, err := s.db.SearchPostsForUser(context.Background(), database.SearchPostsForUserParams{
				UserID:  user.ID,
				Column2: sql.NullString{String: query, Valid: true},
				Limit:   limit,
			})
			if err != nil {
				fmt.Printf("Error searching posts: %v\n", err)
				fmt.Print("Press Enter to continue...")
				reader.ReadString('\n')
				continue
			}

			// Convert search results to regular posts format
			posts = make([]database.GetPostsForUserRow, len(searchResults))
			for i, result := range searchResults {
				posts[i] = database.GetPostsForUserRow{
					ID:          result.ID,
					CreatedAt:   result.CreatedAt,
					UpdatedAt:   result.UpdatedAt,
					Title:       result.Title,
					Url:         result.Url,
					Description: result.Description,
					PublishedAt: result.PublishedAt,
					FeedID:      result.FeedID,
					FeedName:    result.FeedName,
				}
			}

		case "b":
			bookmarks, err := s.db.GetBookmarksForUser(context.Background(), database.GetBookmarksForUserParams{
				UserID: user.ID,
				Limit:  limit,
			})
			if err != nil {
				fmt.Printf("Error getting bookmarks: %v\n", err)
				fmt.Print("Press Enter to continue...")
				reader.ReadString('\n')
				continue
			}

			// Convert bookmarks to regular posts format
			posts = make([]database.GetPostsForUserRow, len(bookmarks))
			for i, bookmark := range bookmarks {
				posts[i] = database.GetPostsForUserRow{
					ID:          bookmark.ID,
					CreatedAt:   bookmark.CreatedAt,
					UpdatedAt:   bookmark.UpdatedAt,
					Title:       bookmark.Title,
					Url:         bookmark.Url,
					Description: bookmark.Description,
					PublishedAt: bookmark.PublishedAt,
					FeedID:      bookmark.FeedID,
					FeedName:    bookmark.FeedName,
				}
			}

		default:
			// Try to parse as post number
			if postNum, err := strconv.Atoi(input); err == nil && postNum >= 1 && postNum <= len(posts) {
				post := posts[postNum-1]
				fmt.Printf("\nOpening: %s\n", post.Title)
				fmt.Printf("URL: %s\n", post.Url)

				if err := openURL(post.Url); err != nil {
					fmt.Printf("Error opening URL: %v\n", err)
					fmt.Printf("Please open this URL manually: %s\n", post.Url)
				} else {
					fmt.Println("Opened in browser!")
				}

				fmt.Print("Press Enter to continue...")
				reader.ReadString('\n')
			} else {
				fmt.Println("Invalid command. Press Enter to continue...")
				reader.ReadString('\n')
			}
		}
	}
}

func main() {
	// Read the config file
	cfg, err := config.Read()
	if err != nil {
		fmt.Printf("Error reading config: %v\n", err)
		os.Exit(1)
	}

	// Open database connection
	db, err := sql.Open("postgres", cfg.DBUrl)
	if err != nil {
		fmt.Printf("Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Create database queries instance
	dbQueries := database.New(db)

	// Create state with config and database
	programState := &state{
		db:  dbQueries,
		cfg: &cfg,
	}

	// Create commands with initialized map
	cmds := &commands{
		handlers: make(map[string]func(*state, command) error),
	}

	// Register commands
	cmds.register("login", handlerLogin)
	cmds.register("register", handlerRegister)
	cmds.register("reset", handlerReset)
	cmds.register("users", handlerUsers)
	cmds.register("agg", handlerAgg)
	cmds.register("addfeed", middlewareLoggedIn(handlerAddFeed))
	cmds.register("feeds", handlerFeeds)
	cmds.register("follow", middlewareLoggedIn(handlerFollow))
	cmds.register("following", middlewareLoggedIn(handlerFollowing))
	cmds.register("unfollow", middlewareLoggedIn(handlerUnfollow))
	cmds.register("browse", middlewareLoggedIn(handlerBrowse))
	cmds.register("search", middlewareLoggedIn(handlerSearch))
	cmds.register("bookmark", middlewareLoggedIn(handlerBookmark))
	cmds.register("unbookmark", middlewareLoggedIn(handlerUnbookmark))
	cmds.register("bookmarks", middlewareLoggedIn(handlerBookmarks))
	cmds.register("tui", middlewareLoggedIn(handlerTUI))

	// Get command-line arguments
	args := os.Args
	if len(args) < 2 {
		fmt.Println("Error: not enough arguments provided")
		os.Exit(1)
	}

	// Create command from arguments
	cmdName := args[1]
	cmdArgs := []string{}
	if len(args) > 2 {
		cmdArgs = args[2:]
	}

	cmd := command{
		name: cmdName,
		args: cmdArgs,
	}

	// Run the command
	err = cmds.run(programState, cmd)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
