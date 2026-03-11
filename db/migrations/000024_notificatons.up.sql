CREATE TABLE notifications (
    id BIGSERIAL PRIMARY KEY,

    -- who created it (NULL = system)
    sender_admin_id BIGINT REFERENCES users(id),

    -- admin | system
    source VARCHAR(10) NOT NULL
      CHECK (source IN ('admin', 'system')),

    title TEXT,
    message TEXT NOT NULL,

    metadata JSONB,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE notification_recipients (
    id BIGSERIAL PRIMARY KEY,

    notification_id BIGINT NOT NULL
      REFERENCES notifications(id)
      ON DELETE CASCADE,

    user_id BIGINT NOT NULL
      REFERENCES users(id)
      ON DELETE CASCADE,

    read BOOLEAN NOT NULL DEFAULT FALSE,
    read_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (notification_id, user_id)
);

CREATE TABLE admin_alerts (
    id BIGSERIAL PRIMARY KEY,

    severity VARCHAR(10) NOT NULL
      CHECK (severity IN ('info', 'warning', 'critical')),

    title TEXT NOT NULL,
    message TEXT NOT NULL,

    source TEXT, -- payments, auth, kyc, infra

    acknowledged BOOLEAN DEFAULT FALSE,
    acknowledged_at TIMESTAMPTZ,
    acknowledged_by BIGINT
      REFERENCES users(id)
      ON DELETE SET NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_notification_recipients_user
  ON notification_recipients (user_id, read);

CREATE INDEX idx_notifications_created
  ON notifications (created_at DESC);

CREATE INDEX idx_admin_alerts_unacked
  ON admin_alerts (acknowledged, severity);

-- sample admin alert
INSERT INTO admin_alerts (
    severity,
    title,
    message,
    source
) VALUES (
    'warning',
    'Sample Alert',
    'This is a sample alert message.',
    'payments'
);