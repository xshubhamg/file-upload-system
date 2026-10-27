DROP TRIGGER IF EXISTS files_set_updated_at ON files;
DROP TRIGGER IF EXISTS users_set_updated_at ON users;
DROP FUNCTION IF EXISTS set_updated_at();

DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS files;
DROP TABLE IF EXISTS users;
