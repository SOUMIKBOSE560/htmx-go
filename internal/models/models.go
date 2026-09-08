package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
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
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Ping verifies database connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// --- Users ---

// sqliteTime converts a timestamp column value into time.Time. SQLite stores
// CURRENT_TIMESTAMP / datetime('now', ...) as "2006-01-02 15:04:05" TEXT,
// which the driver hands back as string/[]byte — database/sql cannot scan
// that directly into time.Time, so timestamp columns are scanned into `any`
// and converted here.
func sqliteTime(v any) (time.Time, error) {
	switch t := v.(type) {
	case nil:
		return time.Time{}, nil
	case time.Time:
		return t, nil
	case string:
		return parseSQLiteTime(t)
	case []byte:
		return parseSQLiteTime(string(t))
	default:
		return time.Time{}, fmt.Errorf("unsupported timestamp type %T", v)
	}
}

func parseSQLiteTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
		time.RFC3339,
		"2006-01-02",
	} {
		if tm, err := time.Parse(layout, s); err == nil {
			return tm, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable timestamp %q", s)
}

// sqliteTimePtr is sqliteTime for nullable columns (NULL/"" -> nil).
func sqliteTimePtr(v any) (*time.Time, error) {
	if v == nil {
		return nil, nil
	}
	if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
		return nil, nil
	}
	if b, ok := v.([]byte); ok && strings.TrimSpace(string(b)) == "" {
		return nil, nil
	}
	tm, err := sqliteTime(v)
	if err != nil {
		return nil, err
	}
	return &tm, nil
}

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (User, error) {
	var u User
	var created any
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)
		 RETURNING id, email, password_hash, created_at`,
		email, passwordHash).Scan(&u.ID, &u.Email, &u.PasswordHash, &created)
	if err != nil {
		return User{}, err
	}
	u.CreatedAt, err = sqliteTime(created)
	return u, err
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	var created any
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE email = $1`,
		email).Scan(&u.ID, &u.Email, &u.PasswordHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u.CreatedAt, err = sqliteTime(created)
	return u, err
}

func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	var u User
	var created any
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE id = $1`,
		id).Scan(&u.ID, &u.Email, &u.PasswordHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u.CreatedAt, err = sqliteTime(created)
	return u, err
}

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, ttl time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, datetime('now', ?))`,
		tokenHash, userID, fmt.Sprintf("+%d seconds", int64(ttl/time.Second)))
	return err
}

// UserForSession resolves a token hash to its user, deleting expired sessions.
func (s *Store) UserForSession(ctx context.Context, tokenHash string) (User, error) {
	var (
		u       User
		created any
		expires any
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT u.id, u.email, u.password_hash, u.created_at, s.expires_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = $1`,
		tokenHash).Scan(&u.ID, &u.Email, &u.PasswordHash, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	if u.CreatedAt, err = sqliteTime(created); err != nil {
		return User{}, err
	}
	expiresAt, err := sqliteTime(expires)
	if err != nil {
		return User{}, err
	}
	if time.Now().After(expiresAt) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
		return User{}, ErrNotFound
	}
	return u, nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
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
	          FROM books WHERE user_id = ?`
	args := []any{userID}
	if f.Status != "" {
		query += ` AND status = ?`
		args = append(args, f.Status)
	}
	if f.Search != "" {
		query += ` AND (lower(title) LIKE lower(?) OR lower(author) LIKE lower(?))`
		args = append(args, "%"+f.Search+"%", "%"+f.Search+"%")
	}
	query += ` ORDER BY created_at DESC LIMIT 200`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []Book
	for rows.Next() {
		var b Book
		var created, finished any
		if err := rows.Scan(&b.ID, &b.UserID, &b.Title, &b.Author, &b.Status,
			&b.Rating, &b.Review, &created, &finished); err != nil {
			return nil, err
		}
		if b.CreatedAt, err = sqliteTime(created); err != nil {
			return nil, err
		}
		if b.FinishedAt, err = sqliteTimePtr(finished); err != nil {
			return nil, err
		}
		books = append(books, b)
	}
	return books, rows.Err()
}

// CreateBook inserts a book, marking finished_at when status is finished.
func (s *Store) CreateBook(ctx context.Context, userID int64, title, author, status string) (Book, error) {
	var b Book
	var created, finished any
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO books (user_id, title, author, status, finished_at)
			 VALUES (?, ?, ?, ?, CASE WHEN ? = 'finished' THEN CURRENT_TIMESTAMP END)
		 RETURNING id, user_id, title, author, status, rating, review, created_at, finished_at`,
		userID, title, author, status, status).
		Scan(&b.ID, &b.UserID, &b.Title, &b.Author, &b.Status, &b.Rating, &b.Review, &created, &finished)
	if err != nil {
		return Book{}, err
	}
	if b.CreatedAt, err = sqliteTime(created); err != nil {
		return Book{}, err
	}
	b.FinishedAt, err = sqliteTimePtr(finished)
	return b, err
}

func (s *Store) BookByID(ctx context.Context, userID, bookID int64) (Book, error) {
	var b Book
	var created, finished any
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, title, author, status, rating, review, created_at, finished_at
		 FROM books WHERE id = $1 AND user_id = $2`,
		bookID, userID).
		Scan(&b.ID, &b.UserID, &b.Title, &b.Author, &b.Status, &b.Rating, &b.Review, &created, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	if err != nil {
		return Book{}, err
	}
	if b.CreatedAt, err = sqliteTime(created); err != nil {
		return Book{}, err
	}
	b.FinishedAt, err = sqliteTimePtr(finished)
	return b, err
}

// UpdateBook applies editable fields. Empty status keeps the current value.
// Transitioning to/from "finished" maintains finished_at.
func (s *Store) UpdateBook(ctx context.Context, userID, bookID int64, title, author, status string, rating *int, review *string) error {
	tag, err := s.db.ExecContext(ctx,
		`UPDATE books SET
			 title = ?, author = ?,
			 status = ?,
			 rating = ?,
			 review = ?,
		   finished_at = CASE
			   WHEN ? = 'finished' AND finished_at IS NULL THEN CURRENT_TIMESTAMP
			   WHEN ? <> 'finished' THEN NULL
		     ELSE finished_at END
		 WHERE id = ? AND user_id = ?`,
		title, author, status, rating, review, status, status, bookID, userID)
	if err != nil {
		return err
	}
	rowsAffected, err := tag.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetBookStatus is a quick single-field update used by the HTMX status select.
func (s *Store) SetBookStatus(ctx context.Context, userID, bookID int64, status string) error {
	tag, err := s.db.ExecContext(ctx,
		`UPDATE books SET status = ?,
		   finished_at = CASE
		     WHEN ? = 'finished' AND finished_at IS NULL THEN CURRENT_TIMESTAMP
		     WHEN ? <> 'finished' THEN NULL
		     ELSE finished_at END
		 WHERE id = ? AND user_id = ?`,
		status, status, status, bookID, userID)
	if err != nil {
		return err
	}
	rowsAffected, err := tag.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteBook(ctx context.Context, userID, bookID int64) error {
	tag, err := s.db.ExecContext(ctx, `DELETE FROM books WHERE id = ? AND user_id = ?`, bookID, userID)
	if err != nil {
		return err
	}
	rowsAffected, err := tag.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Reading goals ---

func (s *Store) GetGoal(ctx context.Context, userID int64, year int) (ReadingGoal, error) {
	var g ReadingGoal
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, year, goal FROM reading_goals WHERE user_id = $1 AND year = $2`,
		userID, year).Scan(&g.UserID, &g.Year, &g.Goal)
	if errors.Is(err, sql.ErrNoRows) {
		return ReadingGoal{}, ErrNotFound
	}
	return g, err
}

// UpsertGoal sets the reading goal for a user/year.
func (s *Store) UpsertGoal(ctx context.Context, userID int64, year, goal int) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO reading_goals (user_id, year, goal)
		 VALUES (?, ?, ?)
		 ON CONFLICT (user_id) DO UPDATE SET goal = excluded.goal, year = excluded.year, updated_at = CURRENT_TIMESTAMP`,
		userID, year, goal)
	return err
}

// --- Stats ---

// StatsFor returns dashboard aggregates for the given user and year.
func (s *Store) StatsFor(ctx context.Context, userID int64, year int) (Stats, error) {
	var st Stats
	st.Year = year
	err := s.db.QueryRowContext(ctx,
		`SELECT
		   sum(CASE WHEN status = 'to_read' THEN 1 ELSE 0 END),
		   sum(CASE WHEN status = 'reading' THEN 1 ELSE 0 END),
		   sum(CASE WHEN status = 'finished' THEN 1 ELSE 0 END),
		   sum(CASE WHEN status = 'finished' AND strftime('%Y', coalesce(finished_at, created_at)) = ? THEN 1 ELSE 0 END),
		   count(*)
		 FROM books WHERE user_id = ?`,
		fmt.Sprintf("%04d", year), userID).Scan(&st.ToRead, &st.Reading, &st.Finished, &st.FinishedYear, &st.Total)
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
