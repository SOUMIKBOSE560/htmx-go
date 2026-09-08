-- Markitdown runs auth-only: users + sessions are the live tables.
-- The book-tracker era tables (books, reading_goals) are unused by every
-- route, so drop them. Indexes die with their tables in SQLite.
DROP TABLE IF EXISTS books;
DROP TABLE IF EXISTS reading_goals;
