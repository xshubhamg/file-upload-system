-- Initial schema for the file-upload service.
-- All timestamps are timezone-aware; IDs are UUIDs generated in the database
-- so direct inserts are safe as well as application-generated IDs.

CREATE TABLE users (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    username      text        NOT NULL,
    email         text        NOT NULL,
    password_hash text        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- Login is case-insensitive, and two users must not differ only by case.
CREATE UNIQUE INDEX users_username_lower_key ON users (lower(username));
CREATE UNIQUE INDEX users_email_lower_key    ON users (lower(email));

CREATE TABLE files (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    object_key   text        NOT NULL,
    filename     text        NOT NULL,
    content_type text        NOT NULL,
    size_bytes   bigint      NOT NULL CHECK (size_bytes >= 0),
    status       text        NOT NULL DEFAULT 'pending'
                             CHECK (status IN ('pending', 'active')),
    completed_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX files_object_key_key ON files (object_key);
-- A user cannot own two live files with the same name. Deleting a row frees
-- the name for reuse, matching the legacy API's 409-on-duplicate behavior.
CREATE UNIQUE INDEX files_user_filename_lower_key ON files (user_id, lower(filename));
CREATE INDEX files_user_created_idx ON files (user_id, created_at DESC, id DESC);
CREATE INDEX files_pending_created_idx ON files (created_at)
    WHERE status = 'pending';

CREATE TABLE refresh_tokens (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash  text        NOT NULL UNIQUE,
    family_id   uuid        NOT NULL,
    user_agent  text        NOT NULL DEFAULT '',
    ip_address  varchar(45) NOT NULL DEFAULT '',
    expires_at  timestamptz NOT NULL,
    revoked_at  timestamptz,
    replaced_by uuid,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX refresh_tokens_user_idx     ON refresh_tokens (user_id);
CREATE INDEX refresh_tokens_family_idx   ON refresh_tokens (family_id);
CREATE INDEX refresh_tokens_expires_idx  ON refresh_tokens (expires_at);
CREATE INDEX refresh_tokens_revoked_idx  ON refresh_tokens (revoked_at)
    WHERE revoked_at IS NOT NULL;

-- Keep updated_at correct even for raw SQL updates.
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER files_set_updated_at
    BEFORE UPDATE ON files
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
