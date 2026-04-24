# Push Notification Issues Found

## Critical Issues

### 1. **Data Payload Contains "title" and "body" (MAIN ISSUE)**
**Location:** Line 93-99 in `SendPush` method

The `data` map includes "title" and "body" keys, which should ONLY be in the `Notification` object:
```go
data := map[string]string{
    "title": info.Title,      // ❌ WRONG
    "body":  info.Message,    // ❌ WRONG
}
```

**Problem:** When FCM receives both a Notification object AND data with "title"/"body" keys, it can cause:
- Message not displaying on Android when app is in foreground
- Unpredictable behavior on iOS
- Data payload being mixed with notification payload

**Fix:** Remove "title" and "body" from data, keep them ONLY in the Notification object.

---

### 2. **AndroidNotification Missing Title and Body**
**Location:** Line 110-114

The AndroidNotification only sets Color and Sound but not Title/Body:
```go
Android: &messaging.AndroidConfig{
    Priority: "high",
    Notification: &messaging.AndroidNotification{
        Color: "#f4bb44",
        Sound: "default",
        // ❌ Missing Title and Body
    },
},
```

**Fix:** Explicitly set Title and Body in AndroidNotification.

---

### 3. **iOS/APNs Missing Critical Headers**
**Location:** Line 117-134

Missing headers that prevent proper notification delivery on iOS:
```go
Headers: map[string]string{
    "apns-priority":  "10",
    "apns-push-type": "alert",
    // ❌ Missing "apns-expiration" and sometimes content-available
},
```

**Fix:** Add proper expiration header.

---

### 4. **Badge Handling Issues**
**Location:** Line 127

```go
Badge: &info.Badge,  // If Badge is 0, this might not work
```

**Problem:** If Badge is 0 (default), the badge won't be set. Should check if Badge > 0.

---

### 5. **Hardcoded Invalid Webpush Icon**
**Location:** Line 140

```go
Icon:  "https://example.com/icon.png",  // ❌ This URL doesn't exist
```

**Fix:** Use a valid icon URL or remove if not needed.

---

## Summary
The main culprit is **#1** - having "title" and "body" in the data payload alongside the Notification object. FCM will accept and log the message as sent, but Android/iOS may silently fail to display it.
