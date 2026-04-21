-- ============================================================
-- CHAT SUPPORT SYSTEM MIGRATION
-- AI + Human Escalation with Real-time Communication
-- ============================================================

-- Support Admin table (extends users with support-specific fields)
CREATE TABLE IF NOT EXISTS "support_admins" (
    "id" UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    "user_id" UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "status" VARCHAR(20) NOT NULL DEFAULT 'offline' CHECK (status IN ('online', 'offline', 'busy')),
    "active_ticket_count" INT NOT NULL DEFAULT 0,
    "max_concurrent_tickets" INT NOT NULL DEFAULT 5,
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id)
);

CREATE TABLE IF NOT EXISTS "tickets" (
    "id" BIGSERIAL PRIMARY KEY,
    "user_id" UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "status" VARCHAR(20) NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'assigned', 'in_progress', 'resolved', 'closed')),
    "assigned_to" UUID REFERENCES support_admins(id) ON DELETE SET NULL,
    "escalation_reason" VARCHAR(50) CHECK (escalation_reason IN ('ai_low_confidence', 'user_request', 'manual', 'out_of_scope', 'complex_query')),
    "priority" VARCHAR(20) NOT NULL DEFAULT 'medium' CHECK (priority IN ('low', 'medium', 'high', 'urgent')),
    "category" VARCHAR(50),
    "resolved_at" TIMESTAMPTZ,
    "first_response_at" TIMESTAMPTZ,
    "average_response_time" INT, -- in seconds
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Chat messages table
CREATE TABLE IF NOT EXISTS "chat_messages" (
    "id" BIGSERIAL PRIMARY KEY,
    "ticket_id" BIGINT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    "sender_id" UUID NOT NULL, -- user_id or support_admin.user_id
    "sender_type" VARCHAR(20) NOT NULL CHECK (sender_type IN ('user', 'admin', 'ai', 'system')),
    "message_text" TEXT NOT NULL,
    "ai_confidence_score" DECIMAL(3,2), -- 0.00 to 1.00
    "metadata" JSONB, -- store additional context like FAQ sources, etc.
    "is_edited" BOOLEAN NOT NULL DEFAULT FALSE,
    "edited_at" TIMESTAMPTZ,
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Attachments table
CREATE TABLE IF NOT EXISTS "attachments" (
    "id" BIGSERIAL PRIMARY KEY,
    "message_id" BIGINT NOT NULL REFERENCES chat_messages(id) ON DELETE CASCADE,
    "file_url" TEXT NOT NULL,
    "file_name" VARCHAR(255) NOT NULL,
    "file_size" INT NOT NULL, -- in bytes
    "mime_type" VARCHAR(100) NOT NULL,
    "type" VARCHAR(20) NOT NULL DEFAULT 'image' CHECK (type IN ('image')),
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- FAQ Knowledge Base for RAG
CREATE TABLE IF NOT EXISTS "faq_documents" (
    "id" BIGSERIAL PRIMARY KEY,
    "title" VARCHAR(255) NOT NULL,
    "content" TEXT NOT NULL,
    "category" VARCHAR(100),
    "tags" TEXT[],
    "embedding_id" VARCHAR(255), -- Reference to vector DB
    "is_active" BOOLEAN NOT NULL DEFAULT TRUE,
    "view_count" INT NOT NULL DEFAULT 0,
    "helpful_count" INT NOT NULL DEFAULT 0,
    "created_by" UUID REFERENCES users(id) ON DELETE SET NULL,
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE faq_documents ADD COLUMN tsv tsvector GENERATED ALWAYS AS (to_tsvector('english', title || ' ' || content)) STORED;
CREATE INDEX idx_faq_documents_tsv ON faq_documents USING gin(tsv);

-- Ticket Assignment History for audit trail
CREATE TABLE IF NOT EXISTS "ticket_assignment_history" (
    "id" BIGSERIAL PRIMARY KEY,
    "ticket_id" BIGINT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    "assigned_from" UUID REFERENCES support_admins(id) ON DELETE SET NULL,
    "assigned_to" UUID REFERENCES support_admins(id) ON DELETE SET NULL,
    "assigned_by" UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    "reason" TEXT,
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Agent Performance Metrics
CREATE TABLE IF NOT EXISTS "agent_metrics" (
    "id" BIGSERIAL PRIMARY KEY,
    "support_admin_id" UUID NOT NULL REFERENCES support_admins(id) ON DELETE CASCADE,
    "tickets_handled" INT NOT NULL DEFAULT 0,
    "tickets_resolved" INT NOT NULL DEFAULT 0,
    "average_resolution_time" INT, -- in seconds
    "average_response_time" INT, -- in seconds
    "customer_satisfaction_score" DECIMAL(3,2), -- 0.00 to 5.00
    "date" DATE NOT NULL,
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(support_admin_id, date)
);

-- Canned Responses for quick replies
CREATE TABLE IF NOT EXISTS "canned_responses" (
    "id" BIGSERIAL PRIMARY KEY,
    "title" VARCHAR(255) NOT NULL,
    "content" TEXT NOT NULL,
    "shortcut" VARCHAR(50) UNIQUE,
    "category" VARCHAR(100),
    "usage_count" INT NOT NULL DEFAULT 0,
    "created_by" UUID REFERENCES users(id) ON DELETE SET NULL,
    "is_active" BOOLEAN NOT NULL DEFAULT TRUE,
    "created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    "updated_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Create indexes for performance
CREATE INDEX idx_tickets_user_id ON tickets(user_id);
CREATE INDEX idx_tickets_assigned_to ON tickets(assigned_to);
CREATE INDEX idx_tickets_status ON tickets(status);
CREATE INDEX idx_tickets_created_at ON tickets(created_at DESC);
CREATE INDEX idx_chat_messages_ticket_id ON chat_messages(ticket_id);
CREATE INDEX idx_chat_messages_created_at ON chat_messages(created_at DESC);
CREATE INDEX idx_attachments_message_id ON attachments(message_id);
CREATE INDEX idx_support_admins_user_id ON support_admins(user_id);
CREATE INDEX idx_support_admins_status ON support_admins(status);
CREATE INDEX idx_faq_documents_category ON faq_documents(category);
CREATE INDEX idx_faq_documents_is_active ON faq_documents(is_active);
CREATE INDEX idx_ticket_assignment_history_ticket_id ON ticket_assignment_history(ticket_id);

-- Trigger to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_support_admins_updated_at BEFORE UPDATE ON support_admins
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_tickets_updated_at BEFORE UPDATE ON tickets
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_faq_documents_updated_at BEFORE UPDATE ON faq_documents
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_canned_responses_updated_at BEFORE UPDATE ON canned_responses
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();