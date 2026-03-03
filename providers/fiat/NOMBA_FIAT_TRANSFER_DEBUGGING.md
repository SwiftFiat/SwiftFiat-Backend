# Nomba Fiat Transfer Debugging Guide

## Error: "nomba: MakeTransfer non-200400"

### What This Means
- HTTP Status Code **400 (Bad Request)** is being returned by Nomba's bank transfer endpoint
- Works fine with test keys but fails with live keys
- Suggests request validation failure on Nomba's side

---

## Common Causes (Test vs Live Differences)

### 1. **Account Validation Strictness**
- **Live API** may validate account numbers and bank codes more strictly
- **Test API** may be more lenient
- Solution: Ensure account number format is exactly correct (no spaces, special chars)

### 2. **Amount Format Issues**
```go
// Current: Converting to kobo (multiply by 100)
amountInKobo := amount.Mul(decimal.NewFromInt(100)).BigInt().Int64()
```
- Verify the **exact amount format** expected by live Nomba API
- Some APIs expect amount in **smallest unit** (kobo), others in **standard unit** (NGN)
- Check Nomba documentation for live API vs test API differences

### 3. **Request Field Validation**
The `NombaBankTransferRequest` requires:
```go
type NombaBankTransferRequest struct {
    Amount        int64  `json:"amount"`          // REQUIRED - in kobo
    AccountNumber string `json:"accountNumber"`   // REQUIRED - no spaces/special chars
    AccountName   string `json:"accountName"`     // REQUIRED - matches resolved name
    BankCode      string `json:"bankCode"`        // REQUIRED - e.g., "057"
    MerchantTxRef string `json:"merchantTxRef"`   // REQUIRED - unique reference
    SenderName    string `json:"senderName"`      // REQUIRED
    Narration     string `json:"narration"`       // OPTIONAL - transfer reason
}
```

Potential issues:
- `AccountName` must **exactly match** the resolved account name from lookup
- `MerchantTxRef` might not be unique or has wrong format
- `SenderName` might be empty or contain invalid characters

### 4. **API Endpoint Differences**
Current code uses: `POST /v1/transfers/bank`

Check if live API requires:
- Different endpoint version (e.g., `/v2/transfers/bank`)
- Additional headers
- Different authentication method

### 5. **Account Lookup Issues**
```go
// This happens during CreateTransferRecipient
recipientInfo, err := s.fiat.CreateTransferRecipient(
    req.AccountNumber,
    req.BankCode,
    req.Name,
)
```
If the lookup succeeds but returns wrong data, MakeTransfer will fail because:
- Resolved `AccountName` might not match what was sent in request
- Account might be invalid in the live system

---

## Recent Improvements Made

### 1. **Better Error Logging** (nomba.go)
```go
// Now logs the full response body instead of just status code
logging.NewLogger().Error("nomba: MakeTransfer non-200", resp.StatusCode, "body", string(bodyBytes))
```
This will show you the exact error message from Nomba's API.

### 2. **Detailed Error Messages in Transaction Service**
```go
s.logger.Infof("MakeTransfer details - recipientCode: %s, amount: %d kobo, accountName: %s, bankCode: %s",
    recipientInfo.RecipientCode, amountInKobo, req.Name, req.BankCode)
```
Before the transfer is attempted, we now log all parameters.

### 3. **Input Validation**
```go
// Now validates required fields early
if req.AccountNumber == "" || req.BankCode == "" || req.Name == "" {
    return nil, fmt.Errorf("invalid request: missing account number, bank code, or recipient name")
}
```

---

## Debugging Steps

### Step 1: Check Live Logs
Look for the enhanced error logging:
```
2026-03-03T11:15:52+01:00 level=error msg="nomba: MakeTransfer non-200" status=400 body={"code":"...","description":"..."}
2026-03-03T11:15:51+01:00 level=info msg="MakeTransfer details" recipientCode="..." amount=... accountName="..." bankCode="..."
```

### Step 2: Verify Request Parameters
From logs, check:
1. Is `amount` a reasonable number (not zero, not negative)?
2. Is `accountNumber` formatted correctly?
3. Does `bankCode` match known bank codes (e.g., "057" for GTBank)?
4. Is `accountName` populated and reasonable?

### Step 3: Test with Nomba Sandbox
If possible, test the exact same request structure with Nomba's sandbox API to isolate the issue.

### Step 4: Check Nomba Documentation
- Confirm `/v1/transfers/bank` is correct endpoint for live
- Check if there's a newer API version
- Verify all field requirements and formats
- Check minimum/maximum amount limits in live (different from test)

### Step 5: Contact Nomba Support
Include:
- Full request payload (sanitized)
- Full error response from their API
- Whether it works with test keys
- Account number format and bank code being used

---

## Potential Quick Fixes

### Check 1: Account Name Normalization
```go
// In HandleBankTransfer, before calling CreateTransferRecipient
// Trim and validate the name
req.Name = strings.TrimSpace(req.Name)
if len(req.Name) < 3 {
    return nil, fmt.Errorf("recipient name too short")
}
```

### Check 2: Amount Limits
Nomba live might have different limits:
```go
// Current limits: 100 - 5,000,000 NGN
// Verify these are correct for live API
if amount.LessThan(decimal.NewFromFloat(100)) || amount.GreaterThan(decimal.NewFromFloat(5000000)) {
    return nil, wallet.ErrAmountNotValidRange
}
```

### Check 3: Verify Account Lookup Works
The `CreateTransferRecipient` calls `ResolveAccount` which also hits Nomba API.
If account lookup returns successfully but with unexpected data, the transfer will fail.

Check the resolved account name matches exactly what's being sent.

---

## Environment Configuration

Verify your live configuration has:
```
NOMBA_CLIENT_ID=<live_client_id>
NOMBA_CLIENT_SECRET=<live_client_secret>
NOMBA_ACCOUNT_ID=<live_account_id>
NOMBA_BASE_URL=https://api.nomba.com/  (or correct live endpoint)
```

Different credentials (test vs live) might point to different API behavior.

---

## Next Steps

1. Run a test transfer with live keys and **capture the full error response** from the new logging
2. Compare the request structure with Nomba's API documentation
3. Verify all account information is being formatted correctly
4. Check if amount handling is different (kobo vs NGN)
5. If still stuck, share the error response with Nomba support team
