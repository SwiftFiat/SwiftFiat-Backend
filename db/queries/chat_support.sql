-- name: CreateSupportAdmin :one
INSERT INTO support_admins (user_id, status, max_concurrent_tickets)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetSupportAdminByUserID :one
SELECT * FROM support_admins WHERE user_id = $1 LIMIT 1;

-- name: GetSupportAdminByID :one
SELECT * FROM support_admins WHERE id = $1 LIMIT 1;

-- name: UpdateSupportAdminStatus :one
UPDATE support_admins
SET status = $2
WHERE id = $1
RETURNING *;

-- name: IncrementActiveTicketCount :exec
UPDATE support_admins
SET active_ticket_count = active_ticket_count + 1
WHERE id = $1;

-- name: DecrementActiveTicketCount :exec
UPDATE support_admins
SET active_ticket_count = active_ticket_count - 1
WHERE id = $1 AND active_ticket_count > 0;

-- name: ListAvailableSupportAdmins :many
SELECT * FROM support_admins
WHERE status IN ('online', 'busy')
AND active_ticket_count < max_concurrent_tickets
ORDER BY active_ticket_count ASC
LIMIT $1;

-- name: ListAllSupportAdmins :many
SELECT sa.*, u.first_name, u.last_name, u.email
FROM support_admins sa
JOIN users u ON sa.user_id = u.id
ORDER BY sa.created_at DESC;

-- ============================================================
-- TICKETS
-- ============================================================

-- name: CreateTicket :one
INSERT INTO tickets (user_id, status, escalation_reason, priority, category)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetTicketByID :one
SELECT * FROM tickets WHERE id = $1 LIMIT 1;

-- name: GetTicketWithUserDetails :one
SELECT t.*, u.first_name, u.last_name, u.email, u.phone_number
FROM tickets t
JOIN users u ON t.user_id = u.id
WHERE t.id = $1
LIMIT 1;

-- name: ListTicketsByUser :many
SELECT * FROM tickets
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListTicketsByStatus :many
SELECT t.*, u.first_name, u.last_name, u.email
FROM tickets t
JOIN users u ON t.user_id = u.id
WHERE t.status = $1
ORDER BY t.created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListTicketsByAssignedAdmin :many
SELECT t.*, u.first_name, u.last_name, u.email
FROM tickets t
JOIN users u ON t.user_id = u.id
WHERE t.assigned_to = $1
ORDER BY t.created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListUnassignedTickets :many
SELECT t.*, u.first_name, u.last_name, u.email
FROM tickets t
JOIN users u ON t.user_id = u.id
WHERE t.assigned_to IS NULL AND t.status = 'open'
ORDER BY t.created_at DESC
LIMIT $1 OFFSET $2;

-- name: UpdateTicketStatus :one
UPDATE tickets
SET status = $2
WHERE id = $1
RETURNING *;

-- name: AssignTicket :one
UPDATE tickets
SET assigned_to = $2, status = 'assigned'
WHERE id = $1
RETURNING *;

-- name: ClaimTicket :one
UPDATE tickets
SET assigned_to = $2, status = 'in_progress'
WHERE id = $1 AND assigned_to IS NULL
RETURNING *;

-- name: ResolveTicket :one
UPDATE tickets
SET status = 'resolved', resolved_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateTicketFirstResponse :exec
UPDATE tickets
SET first_response_at = NOW()
WHERE id = $1 AND first_response_at IS NULL;

-- name: CountTicketsByStatus :one
SELECT COUNT(*) FROM tickets WHERE status = $1;

-- name: CountUserOpenTickets :one
SELECT COUNT(*) FROM tickets
WHERE user_id = $1 AND status IN ('open', 'assigned', 'in_progress');

-- ============================================================
-- CHAT MESSAGES
-- ============================================================

-- name: CreateChatMessage :one
INSERT INTO chat_messages (ticket_id, sender_id, sender_type, message_text, ai_confidence_score, metadata)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetChatMessageByID :one
SELECT * FROM chat_messages WHERE id = $1 LIMIT 1;

-- name: ListChatMessagesByTicket :many
SELECT cm.*, u.first_name, u.last_name, u.email
FROM chat_messages cm
LEFT JOIN users u ON cm.sender_id = u.id
WHERE cm.ticket_id = $1
ORDER BY cm.created_at ASC;

-- name: GetLastMessageByTicket :one
SELECT * FROM chat_messages
WHERE ticket_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: UpdateChatMessage :one
UPDATE chat_messages
SET message_text = $2, is_edited = TRUE, edited_at = NOW()
WHERE id = $1
RETURNING *;

-- name: CountMessagesByTicket :one
SELECT COUNT(*) FROM chat_messages WHERE ticket_id = $1;

-- ============================================================
-- ATTACHMENTS
-- ============================================================

-- name: CreateAttachment :one
INSERT INTO attachments (message_id, file_url, file_name, file_size, mime_type, type)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAttachmentByID :one
SELECT * FROM attachments WHERE id = $1 LIMIT 1;

-- name: ListAttachmentsByMessage :many
SELECT * FROM attachments WHERE message_id = $1;

-- name: DeleteAttachment :exec
DELETE FROM attachments WHERE id = $1;

-- ============================================================
-- FAQ DOCUMENTS
-- ============================================================

-- name: CreateFAQDocument :one
INSERT INTO faq_documents (title, content, category, tags, embedding_id, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetFAQDocumentByID :one
SELECT * FROM faq_documents WHERE id = $1 AND is_active = TRUE LIMIT 1;

-- name: ListFAQDocuments :many
SELECT * FROM faq_documents
WHERE is_active = TRUE
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: ListFAQDocumentsByCategory :many
SELECT * FROM faq_documents
WHERE category = $1 AND is_active = TRUE
ORDER BY created_at DESC;

-- name: SearchFAQDocuments :many
SELECT * FROM faq_documents
WHERE is_active = TRUE
AND (title ILIKE '%' || $1 || '%' OR content ILIKE '%' || $1 || '%')
ORDER BY view_count DESC, helpful_count DESC
LIMIT $2;

-- name: UpdateFAQDocument :one
UPDATE faq_documents
SET title = $2, content = $3, category = $4, tags = $5
WHERE id = $1
RETURNING *;

-- name: IncrementFAQViewCount :exec
UPDATE faq_documents
SET view_count = view_count + 1
WHERE id = $1;

-- name: IncrementFAQHelpfulCount :exec
UPDATE faq_documents
SET helpful_count = helpful_count + 1
WHERE id = $1;

-- name: DeactivateFAQDocument :exec
UPDATE faq_documents
SET is_active = FALSE
WHERE id = $1;

-- ============================================================
-- TICKET ASSIGNMENT HISTORY
-- ============================================================

-- name: CreateTicketAssignmentHistory :one
INSERT INTO ticket_assignment_history (ticket_id, assigned_from, assigned_to, assigned_by, reason)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListTicketAssignmentHistory :many
SELECT tah.*, 
       u_from.first_name as from_first_name, u_from.last_name as from_last_name,
       u_to.first_name as to_first_name, u_to.last_name as to_last_name,
       u_by.first_name as by_first_name, u_by.last_name as by_last_name
FROM ticket_assignment_history tah
LEFT JOIN support_admins sa_from ON tah.assigned_from = sa_from.id
LEFT JOIN users u_from ON sa_from.user_id = u_from.id
LEFT JOIN support_admins sa_to ON tah.assigned_to = sa_to.id
LEFT JOIN users u_to ON sa_to.user_id = u_to.id
LEFT JOIN users u_by ON tah.assigned_by = u_by.id
WHERE tah.ticket_id = $1
ORDER BY tah.created_at DESC;

-- ============================================================
-- AGENT METRICS
-- ============================================================

-- name: UpsertAgentMetrics :one
INSERT INTO agent_metrics (support_admin_id, tickets_handled, tickets_resolved, average_resolution_time, average_response_time, customer_satisfaction_score, date)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (support_admin_id, date)
DO UPDATE SET
    tickets_handled = agent_metrics.tickets_handled + EXCLUDED.tickets_handled,
    tickets_resolved = agent_metrics.tickets_resolved + EXCLUDED.tickets_resolved,
    average_resolution_time = EXCLUDED.average_resolution_time,
    average_response_time = EXCLUDED.average_response_time,
    customer_satisfaction_score = EXCLUDED.customer_satisfaction_score
RETURNING *;

-- name: GetAgentMetricsByDate :one
SELECT * FROM agent_metrics
WHERE support_admin_id = $1 AND date = $2
LIMIT 1;

-- name: ListAgentMetricsByDateRange :many
SELECT * FROM agent_metrics
WHERE support_admin_id = $1
AND date BETWEEN $2 AND $3
ORDER BY date DESC;

-- ============================================================
-- CANNED RESPONSES
-- ============================================================

-- name: CreateCannedResponse :one
INSERT INTO canned_responses (title, content, shortcut, category, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetCannedResponseByID :one
SELECT * FROM canned_responses WHERE id = $1 AND is_active = TRUE LIMIT 1;

-- name: GetCannedResponseByShortcut :one
SELECT * FROM canned_responses WHERE shortcut = $1 AND is_active = TRUE LIMIT 1;

-- name: ListCannedResponses :many
SELECT * FROM canned_responses
WHERE is_active = TRUE
ORDER BY usage_count DESC, title ASC;

-- name: ListCannedResponsesByCategory :many
SELECT * FROM canned_responses
WHERE category = $1 AND is_active = TRUE
ORDER BY usage_count DESC, title ASC;

-- name: IncrementCannedResponseUsage :exec
UPDATE canned_responses
SET usage_count = usage_count + 1
WHERE id = $1;

-- name: UpdateCannedResponse :one
UPDATE canned_responses
SET title = $2, content = $3, shortcut = $4, category = $5
WHERE id = $1
RETURNING *;

-- name: DeactivateCannedResponse :exec
UPDATE canned_responses
SET is_active = FALSE
WHERE id = $1;

-- ============================================================
-- ANALYTICS & REPORTING
-- ============================================================

-- name: GetTicketStatistics :one
SELECT
    COUNT(*) FILTER (WHERE status = 'open') as open_tickets,
    COUNT(*) FILTER (WHERE status = 'assigned') as assigned_tickets,
    COUNT(*) FILTER (WHERE status = 'in_progress') as in_progress_tickets,
    COUNT(*) FILTER (WHERE status = 'resolved') as resolved_tickets,
    COUNT(*) FILTER (WHERE status = 'closed') as closed_tickets,
    AVG(EXTRACT(EPOCH FROM (resolved_at - created_at))) FILTER (WHERE resolved_at IS NOT NULL) as avg_resolution_time
FROM tickets
WHERE created_at >= $1;

-- name: GetAdminWorkload :many
SELECT sa.id, sa.user_id, sa.status, sa.active_ticket_count,
       u.first_name, u.last_name, u.email,
       COUNT(t.id) as total_tickets
FROM support_admins sa
JOIN users u ON sa.user_id = u.id
LEFT JOIN tickets t ON sa.id = t.assigned_to AND t.status IN ('assigned', 'in_progress')
GROUP BY sa.id, u.first_name, u.last_name, u.email
ORDER BY sa.active_ticket_count ASC;