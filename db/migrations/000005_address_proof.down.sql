-- Drop the table
DROP TABLE IF EXISTS proof_of_address_images;

-- Add a comment to indicate this is a down migration
COMMENT ON SCHEMA public IS 'proof_of_address_images and related objects have been removed.';