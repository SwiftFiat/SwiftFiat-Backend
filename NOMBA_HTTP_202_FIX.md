# Nomba Fiat Transfer - HTTP 202 Processing Status Fix

## Problem
The fiat transfer endpoint was failing when Nomba returned HTTP **202 (Accepted)** status code, which indicates the transfer request was accepted and is being processed asynchronously.

Error Log:
```
"nomba: MakeTransfer non-200202" 
Status: 202
Code: "202" 
Description: "Processing"
Status in data: "Processing"
```

## Root Cause
The code only accepted HTTP 200 (OK) as a success response:
```go
if resp.StatusCode != http.StatusOK {
    return nil, fmt.Errorf("nomba: MakeTransfer unexpected status %d", resp.StatusCode)
}
```

However, Nomba's live API returns **202 (Accepted)** for asynchronous transfer processing, which is a valid success state per HTTP standards.

## Solution

### 1. Updated Nomba Provider ([nomba.go](providers/fiat/nomba.go))
- Changed status code check to accept both **200 (OK)** and **202 (Accepted)**
- Added code "202" to the accepted response codes alongside "00" and "200"
- Improved error logging to show actual response body

```go
// Accept both 200 (OK) and 202 (Accepted - processing) as valid responses
if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
    // handle error
}

// Accept codes: "00" (legacy), "200" (success), "202" (processing/accepted)
if result.Code != "00" && result.Code != "200" && result.Code != "202" {
    return nil, fmt.Errorf("nomba: MakeTransfer failed with code %s: %s", result.Code, result.Description)
}
```

### 2. Updated Transaction Service ([transaction_service.go](services/transaction/transaction_service.go))
- Added "processing" to the pending status case in HandleBankTransfer
- Transfers with "Processing" status are now treated as **pending** (awaiting webhook confirmation)
- The system will reconcile the final status when Nomba sends a webhook callback

```go
case "pending", "pending_billing", "processing":
    // Update transaction status to pending
    // Wait for webhook to confirm final status
```

## How It Works Now

1. **User initiates transfer** → Request sent to Nomba
2. **Nomba returns 202 Processing** → Our code accepts this as valid
3. **Transfer status = "pending"** → Stored in database
4. **Wallet is debited** → Amount reserved
5. **User sees "pending" response** → Expects webhook confirmation
6. **Nomba sends webhook** → Background reconciler updates to success/failed
7. **Funds settle** → Either completed or refunded based on webhook

## HTTP Status Codes Reference
- **200 OK**: Request succeeded, immediate response
- **202 Accepted**: Request accepted, processing asynchronously (may complete later)
- **400+ Errors**: Actual failures requiring user action

## Testing
To verify the fix works:
1. Submit a fiat transfer with live Nomba keys
2. Check logs for: `nomba: MakeTransfer` with status 202
3. Verify transfer shows as "pending" in the API response
4. Wait for Nomba webhook to update the status

## Future Considerations
- Set up webhook listener to confirm transfers when Nomba sends status updates
- Add reconciliation job to handle transfers stuck in "pending" state
- Consider retry logic for failed transfers with exponential backoff
