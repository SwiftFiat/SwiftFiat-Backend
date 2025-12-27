-- ============================================================
-- DOWN MIGRATION: CHAT SUPPORT SYSTEM
-- ============================================================

-- ------------------------------------------------------------
-- Drop triggers
-- ------------------------------------------------------------
DROP TRIGGER IF EXISTS update_canned_responses_updated_at ON canned_responses;
DROP TRIGGER IF EXISTS update_faq_documents_updated_at ON faq_documents;
DROP TRIGGER IF EXISTS update_tickets_updated_at ON tickets;
DROP TRIGGER IF EXISTS update_support_admins_updated_at ON support_admins;

-- ------------------------------------------------------------
-- Drop shared trigger function
-- ------------------------------------------------------------
DROP FUNCTION IF EXISTS update_updated_at_column();

-- ------------------------------------------------------------
-- Drop indexes
-- ------------------------------------------------------------
DROP INDEX IF EXISTS idx_ticket_assignment_history_ticket_id;
DROP INDEX IF EXISTS idx_faq_documents_is_active;
DROP INDEX IF EXISTS idx_faq_documents_category;
DROP INDEX IF EXISTS idx_support_admins_status;
DROP INDEX IF EXISTS idx_support_admins_user_id;
DROP INDEX IF EXISTS idx_attachments_message_id;
DROP INDEX IF EXISTS idx_chat_messages_created_at;
DROP INDEX IF EXISTS idx_chat_messages_ticket_id;
DROP INDEX IF EXISTS idx_tickets_created_at;
DROP INDEX IF EXISTS idx_tickets_status;
DROP INDEX IF EXISTS idx_tickets_assigned_to;
DROP INDEX IF EXISTS idx_tickets_user_id;

-- ------------------------------------------------------------
-- Drop tables (order matters due to foreign keys)
-- ------------------------------------------------------------
DROP TABLE IF EXISTS agent_metrics;
DROP TABLE IF EXISTS ticket_assignment_history;
DROP TABLE IF EXISTS attachments;
DROP TABLE IF EXISTS chat_messages;
DROP TABLE IF EXISTS tickets;
DROP TABLE IF EXISTS canned_responses;
DROP TABLE IF EXISTS faq_documents;
DROP TABLE IF EXISTS support_admins;
