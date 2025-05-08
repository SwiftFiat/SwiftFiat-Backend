/**
 * Table: users
 * Purpose: Core table for user account management and authentication
 * 
 * This table stores essential user information, including:
 * - Personal details (name, email, phone)
 * - Multiple authentication methods (password, passcode, PIN)
 * - Account status and role
 * - Audit timestamps
 * 
 * Security considerations:
 * - All authentication fields store hashed values, never plaintext
 * - Email and phone number are unique identifiers
 * - Soft delete functionality via deleted_at
 */
CREATE TABLE IF NOT EXISTS "users" (
    -- Unique identifier for each user
    "id" BIGSERIAL PRIMARY KEY,

    -- User's avatar URL
    "avatar_url" TEXT,

    -- User's avatar BLOB
    "avatar_blob" BYTEA,
    
    -- Personal information 
    -- Optional to allow partial registration
    "first_name" VARCHAR(50),
    "last_name" VARCHAR(50),
    
    -- Primary contact/login identifier
    -- 256 characters to accommodate long emails
    "email" VARCHAR(256) UNIQUE NOT NULL, 
    
    -- Multiple authentication options
    -- All should use secure hashing algorithms (e.g., bcrypt)
    "hashed_password" VARCHAR(256),  -- Main account password
    "hashed_passcode" VARCHAR(256),  -- Secondary auth, possibly for 2FA
    "hashed_pin" VARCHAR(256),       -- Numeric PIN for specific actions
    
    -- Secondary contact identifier
    "phone_number" VARCHAR(50) UNIQUE NOT NULL,
    
    -- User role for authorization
    -- Suggested values: 'user', 'admin', 'staff'
    "role" VARCHAR(10) NOT NULL,
    
    -- Email verification status
    -- Users might need to be verified before full access
    "verified" BOOLEAN NOT NULL DEFAULT FALSE,
    
    -- Audit timestamps
    "created_at" timestamptz NOT NULL DEFAULT (now()),
    "updated_at" timestamptz NOT NULL DEFAULT (now()),
    -- Soft delete implementation
    "deleted_at" timestamptz
);

