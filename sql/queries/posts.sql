-- name: CreatePost :one
INSERT INTO posts (id, created_at, updated_at, title, url, description, published_at, feed_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetPostsForUser :many
SELECT posts.*, feeds.name AS feed_name
FROM posts
INNER JOIN feeds ON posts.feed_id = feeds.id
INNER JOIN feed_follows ON feeds.id = feed_follows.feed_id
WHERE feed_follows.user_id = $1
ORDER BY posts.published_at DESC NULLS LAST, posts.created_at DESC
LIMIT $2;

-- name: GetPostsForUserWithPagination :many
SELECT posts.*, feeds.name AS feed_name
FROM posts
INNER JOIN feeds ON posts.feed_id = feeds.id
INNER JOIN feed_follows ON feeds.id = feed_follows.feed_id
WHERE feed_follows.user_id = $1
AND ($2::TEXT = '' OR feeds.name ILIKE '%' || $2 || '%')
ORDER BY 
  CASE WHEN $3 = 'title' THEN posts.title END ASC,
  CASE WHEN $3 = 'title_desc' THEN posts.title END DESC,
  CASE WHEN $3 = 'published' THEN posts.published_at END ASC NULLS LAST,
  CASE WHEN $3 = 'published_desc' OR $3 = '' THEN posts.published_at END DESC NULLS LAST,
  CASE WHEN $3 = 'feed' THEN feeds.name END ASC,
  CASE WHEN $3 = 'feed_desc' THEN feeds.name END DESC,
  posts.created_at DESC
LIMIT $4 OFFSET $5;

-- name: SearchPostsForUser :many
SELECT posts.*, feeds.name AS feed_name
FROM posts
INNER JOIN feeds ON posts.feed_id = feeds.id
INNER JOIN feed_follows ON feeds.id = feed_follows.feed_id
WHERE feed_follows.user_id = $1
AND (
  posts.title ILIKE '%' || $2 || '%' 
  OR posts.description ILIKE '%' || $2 || '%'
  OR feeds.name ILIKE '%' || $2 || '%'
)
ORDER BY 
  CASE WHEN posts.title ILIKE '%' || $2 || '%' THEN 1 END,
  CASE WHEN feeds.name ILIKE '%' || $2 || '%' THEN 2 END,
  CASE WHEN posts.description ILIKE '%' || $2 || '%' THEN 3 END,
  posts.published_at DESC NULLS LAST,
  posts.created_at DESC
LIMIT $3;