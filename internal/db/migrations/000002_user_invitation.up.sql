-- =========================
-- User invitation / password setup
-- =========================

CREATE TABLE IF NOT EXISTS user_invitation (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  service_provider_id uuid NOT NULL REFERENCES service_provider(id),
  user_id uuid NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,

  token_hash varchar(255) NOT NULL,
  expires_at timestamptz NOT NULL,
  used_at timestamptz,

  created_by uuid NOT NULL REFERENCES "user"(id),
  created_at timestamptz NOT NULL DEFAULT now(),

  UNIQUE (token_hash)
);

CREATE INDEX IF NOT EXISTS idx_user_invitation_user
  ON user_invitation(user_id);

CREATE INDEX IF NOT EXISTS idx_user_invitation_expires
  ON user_invitation(expires_at);
