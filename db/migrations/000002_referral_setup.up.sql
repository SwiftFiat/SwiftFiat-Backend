/**
 * Referral System Schema
 * 
 * This schema manages a user referral system with two main components:
 * 1. A table for users' referral keys
 * 2. A table for tracking actual referrals made
 */

/**
 * Table: referrals
 * Purpose: Stores unique referral keys for users to share with others
 * 
 * Each user can have only one referral key, which they can share
 * with potential new users to track referrals and possibly reward
 * successful referrals.
 */
CREATE TABLE IF NOT EXISTS "referrals" (
    -- Unique identifier for each referral record
    "id" BIGSERIAL PRIMARY KEY,
    
    -- The user who owns this referral key
    -- UNIQUE ensures one referral key per user
    "user_id" INTEGER UNIQUE NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    
    -- The actual referral key/code
    -- This should be a unique, hard-to-guess string
    -- Example: UUID or custom format like "REF-USER123-ABC"
    "referral_key" VARCHAR(256) NOT NULL,
    
    -- Timestamps for record keeping
    "created_at" timestamptz NOT NULL DEFAULT (now()),
    "updated_at" timestamptz NOT NULL DEFAULT (now())
);

/**
 * Table: referral_entries
 * Purpose: Tracks actual referrals made using referral keys
 * 
 * When a new user signs up using someone's referral key,
 * an entry is created in this table to record the relationship
 * and any relevant details.
 */
CREATE TABLE IF NOT EXISTS "referral_entries" (
    -- Unique identifier for each referral entry
    "id" BIGSERIAL PRIMARY KEY,
    
    -- The referral key used
    -- Should match a key from the referrals table
    "referral_key" VARCHAR(256) NOT NULL,
    
    -- The user who made the referral (owner of the referral key)
    -- SET NULL if the referrer is deleted
    "referrer" INTEGER NOT NULL REFERENCES users(id) ON DELETE SET NULL,
    
    -- The user who was referred (signed up using the key)
    -- UNIQUE ensures a user can only be referred once
    -- SET NULL if the referee is deleted
    "referee" INTEGER UNIQUE NOT NULL REFERENCES users(id) ON DELETE SET NULL,
    
    -- Additional details about the referral
    -- Could include: status, reward info, campaign identifier, etc.
    "referral_detail" VARCHAR(256) NOT NULL,
    
    -- Timestamps for record keeping
    "created_at" timestamptz NOT NULL DEFAULT (now()),
    "updated_at" timestamptz NOT NULL DEFAULT (now()),
    -- Soft delete timestamp
    "deleted_at" timestamptz
);