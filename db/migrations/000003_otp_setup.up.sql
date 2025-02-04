/**
 * Table: otps
 * Purpose: Stores One-Time Passwords (OTPs) for user verification processes
 * 
 * This table manages OTP generation, expiration, and validation for various
 * user-related actions such as:
 * - Email verification
 * - Password reset
 * - Two-factor authentication
 * 
 * Security considerations:
 * - OTPs are stored using secure hashing (hence VARCHAR(256) for hash storage)
 * - Each user can only have one active OTP at a time (UNIQUE constraint on user_id)
 * - Automatic cleanup should be implemented for expired OTPs
 */
CREATE TABLE IF NOT EXISTS "otps" (
    -- Unique identifier for each OTP record
    "id" BIGSERIAL PRIMARY KEY,
    
    -- References the user this OTP belongs to
    -- UNIQUE ensures only one active OTP per user
    -- ON DELETE CASCADE ensures cleanup if user is deleted
    "user_id" INTEGER UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    
    -- Stores the hashed OTP value
    -- 256 characters to accommodate various hash algorithms
    "otp" VARCHAR(256) NOT NULL,
    
    -- Indicates if the OTP has expired
    -- Default is TRUE for security - must be explicitly set to FALSE when created
    "expired" BOOLEAN NOT NULL DEFAULT TRUE,
    
    -- Timestamp when the OTP was created
    "created_at" timestamptz NOT NULL DEFAULT (now()),
    
    -- Timestamp when the OTP will expire
    -- Should be set to a future time when OTP is created
    "expires_at" timestamptz NOT NULL DEFAULT (now()),
    
    -- Timestamp for the last update to this record
    "updated_at" timestamptz NOT NULL DEFAULT (now())
);