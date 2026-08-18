package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pageturner/internal/auth"
	"pageturner/internal/models"
)

// pageData carries everything templates need.
type pageData struct {
	User   models.User
	CSRF   string
	Stats  models.Stats
	Books  []models.Book
	Book   models.Book
	Goal   int
	Year   int
	Status string
	Search string
	Error  string
	Email  string
	// Light flips the layout to the light transactional theme (auth pages).
	Light bool
}

// --- Health ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy", "error": err.Error()}) //nolint:errcheck
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// --- Auth pages ---

func (s *Server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	if _, err := r.Cookie("session"); err == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	// The csrf middleware has already ensured a cookie exists; reuse that
	// token rather than rotating it, so open/stale pages keep working.
	render(w, r, "login", pageData{CSRF: csrfFrom(r), Light: true})
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	user, err := s.store.UserByEmail(r.Context(), strings.ToLower(email))
	if err != nil || !auth.CheckPassword(user.PasswordHash, password) {
		render(w, r, "login", pageData{CSRF: csrfFrom(r), Email: email, Error: "Invalid email or password.", Light: true})
		return
	}
	s.startSession(w, r, user)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleRegisterGet(w http.ResponseWriter, r *http.Request) {
	// The csrf middleware has already ensured a cookie exists; reuse that
	// token rather than rotating it, so open/stale pages keep working.
	render(w, r, "register", pageData{CSRF: csrfFrom(r), Light: true})
}

func (s *Server) handleRegisterPost(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	if email == "" || !strings.Contains(email, "@") {
		render(w, r, "register", pageData{CSRF: csrfFrom(r), Email: email, Error: "Please enter a valid email address.", Light: true})
		return
	}
	if len(password) < 8 {
		render(w, r, "register", pageData{CSRF: csrfFrom(r), Email: email, Error: "Password must be at least 8 characters.", Light: true})
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		s.renderError(w, r, "register", "Something went wrong. Please try again.", email)
		return
	}
	user, err := s.store.CreateUser(r.Context(), strings.ToLower(email), hash)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			render(w, r, "register", pageData{CSRF: csrfFrom(r), Email: email, Error: "An account with that email already exists.", Light: true})
			return
		}
		s.renderError(w, r, "register", "Something went wrong. Please try again.", email)
		return
	}
	s.startSession(w, r, user)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("session"); err == nil {
		_ = s.store.DeleteSession(r.Context(), auth.HashToken(c.Value))
	}
	http.SetCookie(w, s.sessionCookie("", -1))
	http.SetCookie(w, s.csrfCookie("", -1))
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// startSession creates a DB session, rotates the CSRF token, and sets cookies.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user models.User) {
	token, err := auth.NewToken()
	if err != nil {
		s.renderError(w, r, "login", "Could not create session. Please try again.", user.Email)
		return
	}
	if err := s.store.CreateSession(r.Context(), auth.HashToken(token), user.ID, s.cfg.SessionTTL); err != nil {
		s.renderError(w, r, "login", "Could not create session. Please try again.", user.Email)
		return
	}
	http.SetCookie(w, s.sessionCookie(token, s.ttlSeconds()))

	csrf := newCSRFOrEmpty()
	http.SetCookie(w, s.csrfCookie(csrf, s.ttlSeconds()))
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, page, msg, email string) {
	render(w, r, page, pageData{CSRF: csrfFrom(r), Error: msg, Email: email, Light: true})
}

// --- Dashboard ---

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	year := time.Now().Year()
	stats, err := s.store.StatsFor(r.Context(), user.ID, year)
	if err != nil {
		s.log.Error("stats", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	books, err := s.store.ListBooks(r.Context(), user.ID, models.BookFilter{})
	if err != nil {
		s.log.Error("list books", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(books) > 5 {
		books = books[:5]
	}
	render(w, r, "dashboard", pageData{
		User:  user,
		CSRF:  csrfFrom(r),
		Stats: stats,
		Books: books,
		Year:  year,
		Goal:  stats.Goal,
	})
}

// --- Books page & search ---

func (s *Server) handleBooks(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	filter := filterFrom(r)
	books, err := s.store.ListBooks(r.Context(), user.ID, filter)
	if err != nil {
		s.log.Error("list books", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	stats, err := s.store.StatsFor(r.Context(), user.ID, time.Now().Year())
	if err != nil {
		s.log.Error("stats", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, "books", pageData{
		User:   user,
		CSRF:   csrfFrom(r),
		Stats:  stats,
		Books:  books,
		Status: filter.Status,
		Search: filter.Search,
	})
}

// handleBooksSearch is the HTMX endpoint for the live search/filter box.
func (s *Server) handleBooksSearch(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	filter := filterFrom(r)
	books, err := s.store.ListBooks(r.Context(), user.ID, filter)
	if err != nil {
		s.log.Error("search books", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, "book_list", pageData{Books: books})
}

func filterFrom(r *http.Request) models.BookFilter {
	return models.BookFilter{
		Status: r.URL.Query().Get("status"),
		Search: strings.TrimSpace(r.URL.Query().Get("q")),
	}
}

// --- Book mutations (HTMX fragments) ---

func (s *Server) handleBookCreate(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	author := strings.TrimSpace(r.FormValue("author"))
	status := r.FormValue("status")

	if title == "" || author == "" {
		// Re-render the add form with the error, swapping it in place.
		w.Header().Set("HX-Retarget", "#add-form")
		w.Header().Set("HX-Reswap", "outerHTML")
		render(w, r, "book_form", pageData{Error: "Title and author are required."})
		return
	}
	if !models.ValidStatus(status) {
		status = models.StatusToRead
	}

	book, err := s.store.CreateBook(r.Context(), user.ID, title, author, status)
	if err != nil {
		s.log.Error("create book", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	s.renderBookCardAndStats(w, r, book, user)
}

func (s *Server) handleBookEdit(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	book, err := s.bookFor(r, user)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	render(w, r, "edit_form", pageData{Book: book, CSRF: csrfFrom(r)})
}

func (s *Server) handleBookUpdate(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	book, err := s.bookFor(r, user)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	author := strings.TrimSpace(r.FormValue("author"))
	status := r.FormValue("status")
	if status == "" {
		status = book.Status
	}
	rating, review := parseRatingReview(r)

	if title == "" || author == "" || !models.ValidStatus(status) {
		render(w, r, "edit_form", pageData{
			Book:  book,
			CSRF:  csrfFrom(r),
			Error: "Title and author are required.",
		})
		return
	}

	if err := s.store.UpdateBook(r.Context(), user.ID, book.ID, title, author, status, rating, review); err != nil {
		s.log.Error("update book", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	updated, err := s.store.BookByID(r.Context(), user.ID, book.ID)
	if err != nil {
		s.log.Error("reload book", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	s.renderBookBodyAndStats(w, r, updated, user)
}

func (s *Server) handleBookStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	book, err := s.bookFor(r, user)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	status := r.FormValue("status")
	if !models.ValidStatus(status) {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	if err := s.store.SetBookStatus(r.Context(), user.ID, book.ID, status); err != nil {
		s.log.Error("set status", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	updated, err := s.store.BookByID(r.Context(), user.ID, book.ID)
	if err != nil {
		s.log.Error("reload book", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	s.renderBookBodyAndStats(w, r, updated, user)
}

func (s *Server) handleBookView(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	book, err := s.bookFor(r, user)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	render(w, r, "book_body", pageData{Book: book})
}

func (s *Server) handleBookDelete(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	book, err := s.bookFor(r, user)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteBook(r.Context(), user.ID, book.ID); err != nil {
		s.log.Error("delete book", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	s.renderStatsOOB(w, r, user)
}

// --- Reading goal ---

func (s *Server) handleGoal(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	year := time.Now().Year()
	goal, err := strconv.Atoi(r.FormValue("goal"))
	if err != nil || goal < 1 || goal > 500 {
		stats, _ := s.store.StatsFor(r.Context(), user.ID, year)
		render(w, r, "goal_panel", pageData{
			User:  user,
			Stats: stats,
			Error: "Goal must be a number between 1 and 500.",
		})
		return
	}
	if err := s.store.UpsertGoal(r.Context(), user.ID, year, goal); err != nil {
		s.log.Error("upsert goal", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	stats, err := s.store.StatsFor(r.Context(), user.ID, year)
	if err != nil {
		s.log.Error("stats", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	render(w, r, "goal_panel", pageData{User: user, Stats: stats})
}

// --- fragment helpers ---

// renderBookCardAndStats returns a new card (for prepend) plus an OOB stats refresh.
func (s *Server) renderBookCardAndStats(w http.ResponseWriter, r *http.Request, book models.Book, user models.User) {
	stats, err := s.store.StatsFor(r.Context(), user.ID, time.Now().Year())
	if err != nil {
		s.log.Error("stats", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	render(w, r, "book_card", pageData{Book: book, Stats: stats})
}

// renderBookBodyAndStats returns the refreshed book body plus OOB stats.
func (s *Server) renderBookBodyAndStats(w http.ResponseWriter, r *http.Request, book models.Book, user models.User) {
	stats, err := s.store.StatsFor(r.Context(), user.ID, time.Now().Year())
	if err != nil {
		s.log.Error("stats", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	render(w, r, "book_body_and_stats", pageData{Book: book, Stats: stats})
}

func (s *Server) renderStatsOOB(w http.ResponseWriter, r *http.Request, user models.User) {
	stats, err := s.store.StatsFor(r.Context(), user.ID, time.Now().Year())
	if err != nil {
		s.log.Error("stats", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	render(w, r, "stats_bar_oob", pageData{Stats: stats})
}

// bookFor loads the current user's book by the {id} path param.
func (s *Server) bookFor(r *http.Request, user models.User) (models.Book, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return models.Book{}, errors.New("bad id")
	}
	return s.store.BookByID(r.Context(), user.ID, id)
}

// parseRatingReview extracts optional rating (1-5) and review text.
func parseRatingReview(r *http.Request) (*int, *string) {
	var rating *int
	if v := r.FormValue("rating"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 5 {
			rating = &n
		}
	}
	var review *string
	if v := strings.TrimSpace(r.FormValue("review")); v != "" {
		review = &v
	}
	return rating, review
}
