# Webhook Replay & Storage - Implementation Guide

## Overview

Storing webhooks in a database for replay is **production best practice** because:

✅ **Audit Trail** - Complete history of all payments  
✅ **Recovery** - Replay if processing failed  
✅ **Debugging** - Test without Cryptomus involvement  
✅ **Compliance** - Required for regulations  
✅ **Testing** - Integration tests with real data  

## Database Schema

### `cryptomus_webhooks` Table
Stores every webhook received:

```sql
- id: UUID (primary key)
- signature: VARCHAR (unique, for deduplication)
- order_id: VARCHAR (for tracking)
- payload: JSONB (full webhook data)
- source_ip: INET (audit trail)
- status: VARCHAR (received → processing → processed → failed)
- processed_transaction_id: UUID (links to transaction)
- retry_count: INT (how many times replayed)
- received_at: TIMESTAMP (when webhook arrived)
- processed_at: TIMESTAMP (when completed)
```

### `webhook_replays` Table
Tracks manual replay attempts:

```sql
- id: UUID (primary key)
- webhook_id: UUID (FK to cryptomus_webhooks)
- replayed_by: VARCHAR (user/system ID)
- reason: VARCHAR (debugging, recovery, etc)
- result: VARCHAR (pending → success → failed)
- replayed_at: TIMESTAMP
```

## Implementation Steps

### 1. Store Webhook on Receipt

```go
// In HandleCryptomusWebhook()

// After signature verification passes:
webhookID, err := c.webhookAudit.StoreWebhook(
    ctx,
    payload.Sign,              // signature (idempotency key)
    payload.OrderID,           // order ID
    rawBody,                   // full JSON payload
    clientIP,                  // source IP
)
if err != nil {
    c.server.logger.Error("webhook_storage_failed", "error", err)
    // Don't fail - still process the webhook
}
```

### 2. Update Status During Processing

```go
// Mark as processing
c.webhookAudit.MarkWebhookProcessing(ctx, webhookID)

// ... do processing ...

// On success
c.webhookAudit.MarkWebhookProcessed(ctx, webhookID, transactionID)

// On failure
c.webhookAudit.MarkWebhookFailed(ctx, webhookID, err.Error())
```

### 3. Prevent Duplicate Processing

**Two-layer protection:**

#### Layer 1: Database Unique Constraint
```sql
UNIQUE (signature)
```
- Signature field is the webhook's unique identifier from Cryptomus
- Prevents inserting same webhook twice

#### Layer 2: Status Check
```go
// Before processing, check if already processed:
existing, _ := c.webhookAudit.GetWebhookBySignature(ctx, payload.Sign)
if existing != nil && existing.Status == "processed" {
    // Already handled - don't reprocess
    ctx.JSON(http.StatusOK, gin.H{"status": "received"})
    return
}
```

## Replay Scenarios

### Scenario 1: System Crash During Processing
```
Webhook Received → Stored → Processing Started → CRASH
           ↓
       [Recovery]
    Replay from DB → Continue from where it left off
```

### Scenario 2: Downstream Service Failure
```
Webhook Received → Stored → Transaction Created → SMS Service Down
           ↓
       [Manual Replay]
    Admin clicks "Replay" → Re-sends SMS notification
```

### Scenario 3: Debugging Payment Issues
```
Customer claims payment not received
           ↓
    Admin views webhook in audit log
           ↓
    Re-processes webhook with tracing enabled
           ↓
    Identifies the root cause
```

## Admin API Endpoints

### List All Webhooks
```bash
GET /api/v1/admin/webhooks?status=processed&page=1&limit=20

Response:
{
    "data": {
        "webhooks": [...],
        "total": 5432,
        "page": 1,
        "pages": 272
    }
}
```

### Replay a Webhook
```bash
POST /api/v1/admin/webhooks/replay

Request:
{
    "webhook_id": "550e8400-e29b-41d4-a716-446655440000",
    "reason": "Recovery from payment processing timeout"
}

Response:
{
    "status": "success",
    "data": {
        "webhook_id": "550e8400-e29b-41d4-a716-446655440000",
        "replay_id": "650e8400-e29b-41d4-a716-446655440001",
        "message": "Webhook has been replayed and processed"
    }
}
```

### View Webhook Details
```bash
GET /api/v1/admin/webhooks/550e8400-e29b-41d4-a716-446655440000

Response:
{
    "data": {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "signature": "abc123def456...",
        "order_id": "order_xyz",
        "payload": {...},
        "status": "processed",
        "processed_at": "2026-06-07T10:30:45Z",
        "retry_count": 2,
        "processed_transaction_id": "txn_123"
    }
}
```

## Safety Mechanisms

### 1. Signature-Based Idempotency
```go
// Signature is unique per webhook from Cryptomus
// Same transaction = same signature = can't process twice
UNIQUE(signature)
```

### 2. Status Tracking
```go
// Four states prevent misunderstanding:
- "received"    → Stored but not processed yet
- "processing"  → Currently being handled
- "processed"   → Complete, don't reprocess
- "failed"      → Error occurred, safe to retry
```

### 3. Retry Count
```go
// Track how many times webhook was replayed
// Alerts on excessive retries (indicates systemic issue)
retry_count = 0  → First attempt (from Cryptomus)
retry_count = 1  → First manual replay
retry_count = 2  → Second manual replay
```

### 4. Linked Transaction
```go
// Once processed, webhook is tied to transaction
// Prevents creating duplicate transactions
processed_transaction_id = txn_123
```

## Common Pitfalls

### ❌ Problem: Replaying Causes Duplicate Transactions
**Solution**: Use status check
```go
if webhook.Status == "processed" {
    return "Already processed"
}
```

### ❌ Problem: Losing Webhook Data
**Solution**: Store raw payload as JSONB
```go
payload JSONB NOT NULL  -- Stores complete original data
```

### ❌ Problem: Can't Trace What Happened
**Solution**: Track status and timestamps
```go
status, received_at, processed_at, processing_error
```

## Monitoring & Alerts

### Key Metrics
```
1. Webhook Success Rate
   (webhooks with status='processed') / total_webhooks

2. Processing Latency
   processed_at - received_at
   Alert if > 60 seconds

3. Failure Rate
   (webhooks with status='failed') / total_webhooks
   Alert if > 1%

4. Retry Count
   AVG(retry_count)
   Alert if > 2 (indicates issues)

5. Unprocessed Webhooks
   COUNT(status='received' AND received_at < NOW()-1hour)
   Alert if > 0
```

### Dashboard Queries

**Failed Webhooks**
```sql
SELECT * FROM cryptomus_webhooks 
WHERE status = 'failed' 
ORDER BY received_at DESC 
LIMIT 20;
```

**Webhooks Requiring Manual Intervention**
```sql
SELECT * FROM cryptomus_webhooks 
WHERE status IN ('received', 'processing')
  AND received_at < NOW() - INTERVAL '5 minutes'
ORDER BY received_at;
```

**Replay History for a Payment**
```sql
SELECT 
    cw.id as webhook_id,
    cw.signature,
    cw.status,
    cw.received_at,
    cw.processed_at,
    wr.replayed_at,
    wr.reason,
    wr.result
FROM cryptomus_webhooks cw
LEFT JOIN webhook_replays wr ON cw.id = wr.webhook_id
WHERE cw.order_id = 'order_123'
ORDER BY cw.received_at DESC;
```

## Redis Enhancement (Optional)

For distributed systems, upgrade deduplication to Redis:

```go
type RedisWebhookValidator struct {
    client *redis.Client
}

func (v *RedisWebhookValidator) CheckDuplicate(signature string) bool {
    exists, _ := v.client.Exists(ctx, "webhook:"+signature).Result()
    return exists > 0
}

func (v *RedisWebhookValidator) MarkProcessed(signature string) {
    // Set with 24-hour TTL
    v.client.SetEX(ctx, "webhook:"+signature, "1", 24*time.Hour)
}
```

Benefits:
- ✅ Works across multiple servers
- ✅ Faster than database queries
- ✅ Automatic cleanup with TTL
- ✅ Real-time deduplication

## Best Practices Checklist

- [ ] Always store raw payload (JSONB)
- [ ] Use signature as unique constraint
- [ ] Track status through entire lifecycle
- [ ] Link to resulting transaction
- [ ] Log all state transitions
- [ ] Have manual replay endpoint (admin only)
- [ ] Monitor replay attempts
- [ ] Alert on processing delays
- [ ] Retain webhooks for audit (30+ days)
- [ ] Test replay functionality regularly
- [ ] Document replay reasons
- [ ] Use Redis for distributed deduplication

## Files Created

1. **db/migrations/webhook_audit.sql** - Database schema
2. **db/webhook_models.go** - Data models
3. **api/webhook_audit.go** - Core audit service
4. **api/webhook_admin.go** - Admin endpoints

## Integration Required

In `HandleCryptomusWebhook()`, after signature verification:

```go
// Store webhook for audit/replay
webhookID, err := c.webhookAudit.StoreWebhook(
    ctx, 
    payload.Sign, 
    payload.OrderID, 
    rawBody, 
    clientIP,
)

// Mark processing
c.webhookAudit.MarkWebhookProcessing(ctx, webhookID)

// ... processing logic ...

// On success
c.webhookAudit.MarkWebhookProcessed(ctx, webhookID, transactionID)

// On error
c.webhookAudit.MarkWebhookFailed(ctx, webhookID, err.Error())
```

---

**Status**: Ready for Implementation  
**Complexity**: Medium (requires database migration + minor code changes)  
**Security Impact**: High (audit trail + replay capability)  
**Performance Impact**: Minimal (~5ms per webhook for storage)
