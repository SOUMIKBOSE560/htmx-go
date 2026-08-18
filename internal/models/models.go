package models

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("not found")

// BookStatus values.
const (
	StatusToRead   = "to_read"
	StatusReading  = "reading"
	StatusFinished = "finished"
)

// ValidStatus reports whether s is a legal book status.
func ValidStatus(s string) bool {
	switch s {
	case StatusToRead, StatusReading, StatusFinished:
		return true
	}
	return false
}

// User is an account row.
type User struct {
	ID           int64
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// Session is a stored login session row.
type Session struct {
	TokenHash string
	UserID    int64
	ExpiresAt time.Time
}

// Book is a single shelf entry.
type Book struct {
	ID         int64
	UserID     int64
	Title      string
	Author     string
	Status     string
	Rating     *int
	Review     *string
	CreatedAt  time.Time
	FinishedAt *time.Time
}

// Stars renders the 1-5 rating as ★/☆ text, or "" when unrated.
func (b Book) Stars() string {
	if b.Rating == nil {
		return ""
	}
	n := *b.Rating
	if n < 1 {
		n = 1
	}
	if n > 5 {
		n = 5
	}
	return strings.Repeat("★", n) + strings.Repeat("☆", 5-n)
}

// RatingValue returns the rating as a plain int (0 when unrated).
func (b Book) RatingValue() int {
	if b.Rating == nil {
		return 0
	}
	return *b.Rating
}

// ReviewText returns the review text, or "" when absent.
func (b Book) ReviewText() string {
	if b.Review == nil {
		return ""
	}
	return *b.Review
}

// FinishedDate returns the formatted completion date, or "" when not finished.
func (b Book) FinishedDate() string {
	if b.FinishedAt == nil {
		return ""
	}
	return b.FinishedAt.Format("Jan 2, 2006")
}

// ReadingGoal is a yearly reading target.
type ReadingGoal struct {
	UserID int64
	Year   int
	Goal   int
}

// Stats aggregates a user's shelf for the dashboard.
type Stats struct {
	Total        int
	ToRead       int
	Reading      int
	Finished     int
	Goal         int
	FinishedYear int
	Year         int
}

// Progress returns the percentage (0-100) of this year's goal completed.
func (s Stats) Progress() int {
	if s.Goal <= 0 {
		return 0
	}
	p := s.FinishedYear * 100 / s.Goal
	if p > 100 {
		return 100
	}
	return p
}

// Store wraps all data access for the app.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Ping verifies database connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// --- Users ---

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)
		 RETURNING id, email, password_hash, created_at`,
		email, passwordHash).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	return u, err
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE email = $1`,
		email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE id = $1`,
		id).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, ttl time.Duration) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, now() + $3::interval)`,
		tokenHash, userID, ttl.String())
	return err
}

// UserForSession resolves a token hash to its user, deleting expired sessions.
func (s *Store) UserForSession(ctx context.Context, tokenHash string) (User, error) {
	var (
		u       User
		expires time.Time
	)
	err := s.pool.QueryRow(ctx,
		`SELECT u.id, u.email, u.password_hash, u.created_at, s.expires_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = $1`,
		tokenHash).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	if time.Now().After(expires) {
		_, _ = s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
		return User{}, ErrNotFound
	}
	return u, nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

// --- Books ---

// BookFilter narrows a book listing.
type BookFilter struct {
	Status string
	Search string
}

// ListBooks returns a user's books, newest first, optionally filtered.
func (s *Store) ListBooks(ctx context.Context, userID int64, f BookFilter) ([]Book, error) {
	query := `SELECT id, user_id, title, author, status, rating, review, created_at, finished_at
	          FROM books WHERE user_id = $1`
	args := []any{userID}
	argn := 2
	if f.Status != "" {
		query += ` AND status = $` + itoa(argn)
		args = append(args, f.Status)
		argn++
	}
	if f.Search != "" {
		query += ` AND (title ILIKE $` + itoa(argn) + ` OR author ILIKE $` + itoa(argn) + `)`
		args = append(args, "%"+f.Search+"%")
		argn++
	}
	query += ` ORDER BY created_at DESC LIMIT 200`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var b Book
		if err := rows.Scan(&b.ID, &b.UserID, &b.Title, &b.Author, &b.Status,
			&b.Rating, &b.Review, &b.CreatedAt, &b.FinishedAt); err != nil {
			return nil, err
		}
		books = append(books, b)
	}
	return books, rows.Err()
}

// CreateBook inserts a book, marking finished_at when status is finished.
func (s *Store) CreateBook(ctx context.Context, userID int64, title, author, status string) (Book, error) {
	var b Book
	err := s.pool.QueryRow(ctx,
		`INSERT INTO books (user_id, title, author, status, finished_at)
		 VALUES ($1, $2, $3, $4, CASE WHEN $4 = 'finished' THEN now() END)
		 RETURNING id, user_id, title, author, status, rating, review, created_at, finished_at`,
		userID, title, author, status).
		Scan(&b.ID, &b.UserID, &b.Title, &b.Author, &b.Status, &b.Rating, &b.Review, &b.CreatedAt, &b.FinishedAt)
	return b, err
}

func (s *Store) BookByID(ctx context.Context, userID, bookID int64) (Book, error) {
	var b Book
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, title, author, status, rating, review, created_at, finished_at
		 FROM books WHERE id = $1 AND user_id = $2`,
		bookID, userID).
		Scan(&b.ID, &b.UserID, &b.Title, &b.Author, &b.Status, &b.Rating, &b.Review, &b.CreatedAt, &b.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	return b, err
}

// UpdateBook applies editable fields. Empty status keeps the current value.
// Transitioning to/from "finished" maintains finished_at.
func (s *Store) UpdateBook(ctx context.Context, userID, bookID int64, title, author, status string, rating *int, review *string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE books SET
		   title = $3, author = $4,
		   status = $5,
		   rating = $6,
		   review = $7,
		   finished_at = CASE
		     WHEN $5 = 'finished' AND finished_at IS NULL THEN now()
		     WHEN $5 <> 'finished' THEN NULL
		     ELSE finished_at END
		 WHERE id = $1 AND user_id = $2`,
		bookID, userID, title, author, status, rating, review)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetBookStatus is a quick single-field update used by the HTMX status select.
func (s *Store) SetBookStatus(ctx context.Context, userID, bookID int64, status string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE books SET status = $3,
		   finished_at = CASE
		     WHEN $3 = 'finished' AND finished_at IS NULL THEN now()
		     WHEN $3 <> 'finished' THEN NULL
		     ELSE finished_at END
		 WHERE id = $1 AND user_id = $2`,
		bookID, userID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteBook(ctx context.Context, userID, bookID int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM books WHERE id = $1 AND user_id = $2`, bookID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Reading goals ---

func (s *Store) GetGoal(ctx context.Context, userID int64, year int) (ReadingGoal, error) {
	var g ReadingGoal
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, year, goal FROM reading_goals WHERE user_id = $1 AND year = $2`,
		userID, year).Scan(&g.UserID, &g.Year, &g.Goal)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReadingGoal{}, ErrNotFound
	}
	return g, err
}

// UpsertGoal sets the reading goal for a user/year.
func (s *Store) UpsertGoal(ctx context.Context, userID int64, year, goal int) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO reading_goals (user_id, year, goal)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id) DO UPDATE SET goal = EXCLUDED.goal, year = EXCLUDED.year, updated_at = now()`,
		userID, year, goal)
	return err
}

// --- Stats ---

// StatsFor returns dashboard aggregates for the given user and year.
func (s *Store) StatsFor(ctx context.Context, userID int64, year int) (Stats, error) {
	var st Stats
	st.Year = year
	err := s.pool.QueryRow(ctx,
		`SELECT
		   count(*) FILTER (WHERE status = 'to_read'),
		   count(*) FILTER (WHERE status = 'reading'),
		   count(*) FILTER (WHERE status = 'finished'),
		   count(*) FILTER (WHERE status = 'finished' AND extract(year FROM coalesce(finished_at, created_at)) = $2),
		   count(*)
		 FROM books WHERE user_id = $1`,
		userID, year).Scan(&st.ToRead, &st.Reading, &st.Finished, &st.FinishedYear, &st.Total)
	if err != nil {
		return Stats{}, err
	}
	g, err := s.GetGoal(ctx, userID, year)
	if err == nil {
		st.Goal = g.Goal
	} else if !errors.Is(err, ErrNotFound) {
		return Stats{}, err
	}
	return st, nil
}

// itoa is a tiny helper to keep SQL placeholder numbering readable.
func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
