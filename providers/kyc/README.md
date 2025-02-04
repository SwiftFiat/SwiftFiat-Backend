# Know Your Customer (KYC) System

## Overview

This document outlines the multi-tiered Know Your Customer (KYC) system implemented in our application. The system features three levels of verification, each offering increased transaction limits and wallet capabilities while ensuring regulatory compliance and user security.

## KYC Levels

### Level 1
- **Transaction Limits:**
  - Daily Transfer: ₦50,000
  - Maximum Wallet Balance: ₦200,000

#### Required Information:
- Full Name
- Phone Number
- Email
- BVN or NIN
- Gender
- Selfie (with liveness check)

#### Validation Process:
- Confirms name matches BVN or NIN record

### Level 2
- **Transaction Limits:**
  - Daily Transfer: ₦200,000
  - Maximum Wallet Balance: ₦500,000

#### Required Information:
- Additional ID Verification:
  - If BVN was provided in Level 1, NIN is required
  - If NIN was provided in Level 1, BVN is required
- Physical ID (one of the following):
  - International Passport
  - Voter's Card
  - Driver's License
- Complete Address:
  1. State
  2. Local Government Area (LGA)
  3. House Number
  4. Street Name
  5. Nearest Landmark

#### Validation Process:
- Confirms name on physical ID
- Verifies validity of provided ID

### Level 3
- **Transaction Limits:**
  - Daily Transfer: ₦5,000,000
  - Maximum Wallet Balance: Unlimited

#### Required Information:
- Proof of Address (one of the following):
  - Utility Bill
  - Bank Statement
  - Tenancy Agreement

**Note:** Documents must not be older than 3 months

## Implementation Details

### Database Schema

The KYC information is stored in a `kyc` table with the following key features:
- Unique user identification
- Tiered structure (0-3)
- Separate fields for each verification level
- Timestamps for creation and updates
- JSON field for extensibility

### Security Considerations

1. All sensitive data is encrypted at rest
2. Access to KYC data is strictly controlled and audited
3. Documents are stored securely with limited access

## Validation Process

1. **Document Verification:**
   - Automated checks for document authenticity
   - Manual review for suspicious cases

2. **Data Validation:**
   - Cross-reference with government databases
   - Consistency checks across provided documents

## API Endpoints

Document the endpoints used for:
1. Submitting KYC information
2. Checking KYC status
3. Upgrading KYC level

## Testing

Outline the testing strategy:
1. Unit tests for validation logic
2. Integration tests for API endpoints
3. End-to-end tests for the complete KYC flow

## Troubleshooting

Common issues and their solutions:
1. Document upload failures
2. Validation errors
3. Upgrade issues

## Regulatory Compliance

This KYC system is designed to comply with:
- Local financial regulations
- Anti-Money Laundering (AML) guidelines
- Data protection laws

## Future Improvements

Potential enhancements:
1. Automated ID verification
2. Additional verification methods
3. Enhanced fraud detection

---

For technical support or clarification, please contact the development team.