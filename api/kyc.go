package api

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/kyc"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/audit"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

type KYC struct {
	server  *Server
	notifyr *service.Notification
	push    *service.PushNotificationService
	email   *service.Plunk
	audit   *audit.Service
}

func (k KYC) router(server *Server) {
	k.server = server
	k.notifyr = service.NewNotificationService(k.server.queries, k.server.logger, k.server.pushNotification)
	k.push = server.pushNotification
	k.email = server.emailService
	k.audit = server.auditService

	serverGroupV1 := server.router.Group("/api/v1/kyc")
	serverGroupV1.GET("", k.server.authMiddleware.AuthenticatedMiddleware(), k.getUserKyc)
	serverGroupV1.POST("validate-bvn", k.server.authMiddleware.AuthenticatedMiddleware(), k.validateBVN)
	serverGroupV1.POST("validate-nin", k.server.authMiddleware.AuthenticatedMiddleware(), k.validateNIN)
	serverGroupV1.POST("upload-address-proof", k.server.authMiddleware.AuthenticatedMiddleware(), k.verifyUtilityBill)
	// serverGroupV1.POST("verify-utility-bill", k.server.authMiddleware.AuthenticatedMiddleware(), k.verifyUtilityBill)
	serverGroupV1.GET("retrieve-address-proof/:id", k.server.authMiddleware.AuthenticatedMiddleware(), k.retrieveProofOfAddress)

	// New endpoint to check verification progress
	serverGroupV1.GET("verification-progress", k.server.authMiddleware.AuthenticatedMiddleware(), k.getVerificationProgress)

	// Admin endpoints
	serverGroupV1.POST("/admin/verify/:id", k.server.authMiddleware.AuthenticatedMiddleware(), k.verifyKYC)
	serverGroupV1.POST("/admin/reject/:id", k.server.authMiddleware.AuthenticatedMiddleware(), k.rejectKYC)
	serverGroupV1.GET("/admin/user/:id", k.server.authMiddleware.AuthenticatedMiddleware(), k.getAdminUserKyc)
	serverGroupV1.GET("/admin/all", k.server.authMiddleware.AuthenticatedMiddleware(), k.listAllUserKyc)
}

// getVerificationProgress returns which fields are completed and what's still needed
func (k *KYC) getVerificationProgress(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	userKyc, err := k.server.queries.GetKYCByUserID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("No KYC record found", gin.H{
			"completed_fields": []string{},
			"pending_fields": []string{
				"full_name", "phone_number", "email", "bvn_or_nin",
				"gender", "selfie", "government_id", "address", "proof_of_address",
			},
			"progress_percentage": 0,
			"is_verified":         false,
		}))
		return
	} else if err != nil {
		k.server.logger.Errorf("get progress failed [kyc/getVerificationProgress]: %v", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Calculate progress
	completedFields := []string{}
	pendingFields := []string{}
	totalFields := 9
	completedCount := 0

	// Check each requirement
	if userKyc.FullName.Valid && userKyc.FullName.String != "" {
		completedFields = append(completedFields, "full_name")
		completedCount++
	} else {
		pendingFields = append(pendingFields, "full_name")
	}

	if userKyc.PhoneNumber.Valid && userKyc.PhoneNumber.String != "" {
		completedFields = append(completedFields, "phone_number")
		completedCount++
	} else {
		pendingFields = append(pendingFields, "phone_number")
	}

	if userKyc.Email.Valid && userKyc.Email.String != "" {
		completedFields = append(completedFields, "email")
		completedCount++
	} else {
		pendingFields = append(pendingFields, "email")
	}

	if (userKyc.Bvn.Valid && userKyc.Bvn.String != "") || (userKyc.Nin.Valid && userKyc.Nin.String != "") {
		completedFields = append(completedFields, "bvn_or_nin")
		completedCount++
	} else {
		pendingFields = append(pendingFields, "bvn_or_nin")
	}

	if userKyc.Gender.Valid && userKyc.Gender.String != "" {
		completedFields = append(completedFields, "gender")
		completedCount++
	} else {
		pendingFields = append(pendingFields, "gender")
	}

	if userKyc.SelfieUrl.Valid && userKyc.SelfieUrl.String != "" {
		completedFields = append(completedFields, "selfie")
		completedCount++
	} else {
		pendingFields = append(pendingFields, "selfie")
	}

	if userKyc.IDType.Valid && userKyc.IDNumber.Valid && userKyc.IDImageUrl.Valid {
		completedFields = append(completedFields, "government_id")
		completedCount++
	} else {
		pendingFields = append(pendingFields, "government_id")
	}

	if userKyc.State.Valid && userKyc.Lga.Valid && userKyc.HouseNumber.Valid && userKyc.StreetName.Valid && userKyc.PostalCode.Valid {
		completedFields = append(completedFields, "address")
		completedCount++
	} else {
		pendingFields = append(pendingFields, "address")
	}

	if userKyc.ProofOfAddressType.Valid && userKyc.ProofOfAddressUrl.Valid && userKyc.ProofOfAddressDate.Valid {
		completedFields = append(completedFields, "proof_of_address")
		completedCount++
	} else {
		pendingFields = append(pendingFields, "proof_of_address")
	}

	progressPercentage := (completedCount * 100) / totalFields

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("KYC Verification Progress", gin.H{
		"completed_fields":    completedFields,
		"pending_fields":      pendingFields,
		"progress_percentage": progressPercentage,
		"is_verified":         userKyc.Status == "verified",
		"status":              userKyc.Status,
	}))
}

func (k *KYC) getUserKyc(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	userKyc, err := k.server.queries.GetKYCByUserID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNoKYC))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	k.decryptKyc(&userKyc)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("User KYC Information Fetched Successfully", models.ToUserKYCInformation(&userKyc)))
}

func (k *KYC) validateBVN(ctx *gin.Context) {
	request := struct {
		BVN string `json:"bvn" binding:"required"`
		// FirstName string `json:"first_name"`
		// LastName  string `json:"last_name"`
		// DOB       string `json:"dob"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		k.server.logger.Log(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidBVNInput))
		return
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	userKYC, err := k.server.queries.GetKYCByUserID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("User KYC not found"))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	if userKYC.Tier != "tier_1" {
		ctx.JSON(http.StatusConflict, basemodels.NewError("You have to be on tier 1 to start tier 2 verifications"))
		return
	}

	dbUser, err := k.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	provider, exists := k.server.provider.GetProvider(providers.Dojah)
	if !exists {
		k.server.logger.Error("Dojah Provider not found")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	kycProvider, ok := provider.(*kyc.DOJAHProvider)
	if !ok {
		k.server.logger.Error("Cannot convert provider to DOJAHProvider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Lookup BVN full details first
	lookupData, err := kycProvider.LookupBVN(request.BVN)
	if err != nil {
		k.server.logger.Errorf("BVN Lookup failed (non-fatal): %v", err)
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(fmt.Sprintf("BVN Lookup Failure: %s", err)))
		return
	} else {
		// Update user first name and last name with lookup data
		if lookupData.FirstName != "" {
			_, err = k.server.queries.UpdateUserFirstName(ctx, db.UpdateUserFirstNameParams{
				ID: dbUser.ID,
				FirstName: sql.NullString{
					String: lookupData.FirstName,
					Valid:  true,
				},
			})
			if err != nil {
				k.server.logger.Errorf("failed to update user %d first name from BVN lookup: %v", dbUser.ID, err)
			}
		}

		if lookupData.LastName != "" {
			_, err = k.server.queries.UpdateUserLastName(ctx, db.UpdateUserLastNameParams{
				ID: dbUser.ID,
				LastName: sql.NullString{
					String: lookupData.LastName,
					Valid:  true,
				},
			})
			if err != nil {
				k.server.logger.Errorf("failed to update user %d last name from BVN lookup: %v", dbUser.ID, err)
			}
		}
	}

	verificationData, err := kycProvider.ValidateBVN(request.BVN)
	if err != nil {
		k.server.logger.Error(err)
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(fmt.Sprintf("BVN Validation Failure: %s", err)))
		return
	}

	// Get or create KYC record
	userKyc, err := k.server.queries.GetKYCByUserID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		userKyc, err = k.server.queries.CreateNewKYC(ctx, activeUser.UserID)
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
			return
		}
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Update KYC with BVN information
	args := db.UpdateBVNParams{
		ID: userKyc.ID,
		Bvn: sql.NullString{
			String: utils.Encrypt(verificationData.BVN.Value, k.server.config.SigningKey),
			Valid:  verificationData.BVN.Status,
		},
	}

	kyc, err := k.server.queries.UpdateBVN(ctx, args)
	if err != nil {
		k.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("KYC update failed at DB level"))
		return
	}

	// Update to Tier 2 if both BVN and NIN are present
	if kyc.Bvn.Valid && kyc.Bvn.String != "" && kyc.Nin.Valid && kyc.Nin.String != "" {
		if kyc.Tier == "tier_1" {
			_, err = k.server.queries.UpdateKYCToTierTwo(ctx, kyc.ID)
			if err != nil {
				k.server.logger.Errorf("failed to update kyc %d to tier 2: %v", kyc.ID, err)
			}
		}

		_, err = k.server.queries.UpdateKYCStatus(ctx, db.UpdateKYCStatusParams{
			ID:     kyc.ID,
			Status: "verified",
		})
		if err != nil {
			k.server.logger.Errorf("failed to update kyc %d status to verified: %v", kyc.ID, err)
		}

		_, err = k.server.queries.UpdateUserKYCVerificationStatus(ctx, db.UpdateUserKYCVerificationStatusParams{
			ID:            kyc.UserID,
			IsKycVerified: true,
			UpdatedAt:     time.Now(),
		})
		if err != nil {
			k.server.logger.Errorf("failed to update user %d kyc verification status: %v", kyc.UserID, err)
		}

		// Refresh kyc object
		updatedKyc, err := k.server.queries.GetKYCByUserID(ctx, kyc.UserID)
		if err == nil {
			kyc = updatedKyc
		}

		// Send notifications
		go func() {
			bgCtx := context.Background()
			k.email.KycVerified(bgCtx, dbUser.FirstName.String, dbUser.Email)
			k.notifyr.CreateWithRecipients(bgCtx, nil, "KYC Verified", "Your identity verification (Tier 2) was successful.", "system", []uuid.UUID{kyc.UserID})
			k.push.SendPushNotification(bgCtx, kyc.UserID, "KYC Verified", "Your identity verification (Tier 2) was successful.")
			k.email.KycVerified(bgCtx, dbUser.FirstName.String, dbUser.Email)
		}()
	} else {
		// Only BVN verified
		go func() {
			bgCtx := context.Background()
			k.notifyr.CreateWithRecipients(bgCtx, nil, "BVN Verified", "Your BVN has been verified. Please verify your NIN to complete Tier 2 verification.", "system", []uuid.UUID{kyc.UserID})
			k.push.SendPushNotification(bgCtx, kyc.UserID, "BVN Verified", "Your BVN has been verified. Please verify your NIN to complete Tier 2 verification.")
		}()
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("BVN verified successfully", nil))
}

func (k *KYC) validateNIN(ctx *gin.Context) {
	request := struct {
		NIN    string `json:"nin" binding:"required"`
		Selfie string `json:"selfie_image" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		k.server.logger.Log(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidNINInput))
		return
	}

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	userKYC, err := k.server.queries.GetKYCByUserID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("User KYC not found"))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	if userKYC.Tier != "tier_1" {
		ctx.JSON(http.StatusConflict, basemodels.NewError("You have to be on tier 1 to start tier 2 verifications"))
		return
	}

	dbUser, err := k.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	provider, exists := k.server.provider.GetProvider(providers.Dojah)
	if !exists {
		k.server.logger.Error("Dojah Provider not found")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	kycProvider, ok := provider.(*kyc.DOJAHProvider)
	if !ok {
		k.server.logger.Error("Cannot convert provider to DOJAHProvider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ninRequest := map[string]any{
		"nin":          request.NIN,
		"selfie_image": request.Selfie,
	}

	verificationData, err := kycProvider.ValidateNIN(ninRequest)
	if err != nil {
		k.server.logger.Error(err)
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(fmt.Sprintf("NIN Validation Failure: %s", err)))
		return
	}

	k.server.logger.Log(logrus.InfoLevel, "NIN Verification Data: ", verificationData)

	// Validate selfie match
	if !verificationData.SelfieVerification.Match {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Selfie verification failed. Please ensure your face is clearly visible."))
		return
	}

	// Compare NIN names with user's stored names (from BVN lookup)
	if dbUser.FirstName.Valid && dbUser.FirstName.String != "" {
		if !strings.EqualFold(verificationData.FirstName, dbUser.FirstName.String) {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(fmt.Sprintf("First name from NIN (%s) does not match your registered first name on BVN (%s)", verificationData.FirstName, dbUser.FirstName.String)))
			return
		}
	}

	if dbUser.LastName.Valid && dbUser.LastName.String != "" {
		if !strings.EqualFold(verificationData.LastName, dbUser.LastName.String) {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError(fmt.Sprintf("Last name from NIN (%s) does not match your registered last name on BVN (%s)", verificationData.LastName, dbUser.LastName.String)))
			return
		}
	}

	// Get or create KYC record
	userKyc, err := k.server.queries.GetKYCByUserID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		userKyc, err = k.server.queries.CreateNewKYC(ctx, activeUser.UserID)
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
			return
		}
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Update KYC with NIN information
	args := db.UpdateKYCNINInfoParams{
		ID: userKyc.ID,
		Nin: sql.NullString{
			String: utils.Encrypt(verificationData.NIN, k.server.config.SigningKey),
			Valid:  true,
		},
		Gender: sql.NullString{
			String: utils.Encrypt(strings.ToLower(verificationData.Gender), k.server.config.SigningKey),
			Valid:  true,
		},
		SelfieUrl: sql.NullString{
			String: utils.Encrypt(verificationData.Image, k.server.config.SigningKey),
			Valid:  true,
		},
		PhoneNumber: sql.NullString{
			String: utils.Encrypt(verificationData.PhoneNumber, k.server.config.SigningKey),
			Valid:  true,
		},
		FullName: sql.NullString{
			String: utils.Encrypt(verificationData.FirstName+" "+verificationData.LastName, k.server.config.SigningKey),
			Valid:  true,
		},
	}

	kyc, err := k.server.queries.UpdateKYCNINInfo(ctx, args)
	if err != nil {
		k.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("KYC update failed at DB level"))
		return
	}

	// Update to Tier 2 if both BVN and NIN are present
	if kyc.Bvn.Valid && kyc.Bvn.String != "" && kyc.Nin.Valid && kyc.Nin.String != "" {
		if kyc.Tier == "tier_1" {
			_, err = k.server.queries.UpdateKYCToTierTwo(ctx, kyc.ID)
			if err != nil {
				k.server.logger.Errorf("failed to update kyc %d to tier 2: %v", kyc.ID, err)
			}
		}

		_, err = k.server.queries.UpdateKYCStatus(ctx, db.UpdateKYCStatusParams{
			ID:     kyc.ID,
			Status: "verified",
		})
		if err != nil {
			k.server.logger.Errorf("failed to update kyc %d status to verified: %v", kyc.ID, err)
		}

		_, err = k.server.queries.UpdateUserKYCVerificationStatus(ctx, db.UpdateUserKYCVerificationStatusParams{
			ID:            kyc.UserID,
			IsKycVerified: true,
			UpdatedAt:     time.Now(),
		})
		if err != nil {
			k.server.logger.Errorf("failed to update user %d kyc verification status: %v", kyc.UserID, err)
		}

		// Refresh kyc object
		updatedKyc, err := k.server.queries.GetKYCByUserID(ctx, kyc.UserID)
		if err == nil {
			kyc = updatedKyc
		}

		// Send notifications
		go func() {
			bgCtx := context.Background()
			k.email.KycVerified(bgCtx, dbUser.FirstName.String, dbUser.Email)
			k.notifyr.CreateWithRecipients(bgCtx, nil, "KYC Verified", "Your identity verification (Tier 2) was successful.", "system", []uuid.UUID{kyc.UserID})
			k.push.SendPushNotification(bgCtx, kyc.UserID, "KYC Verified", "Your identity verification (Tier 2) was successful.")
			k.email.KycVerified(bgCtx, dbUser.FirstName.String, dbUser.Email)
		}()
	} else {
		// Only NIN verified
		go func() {
			bgCtx := context.Background()
			k.notifyr.CreateWithRecipients(bgCtx, nil, "NIN Verified", "Your NIN has been verified. Please verify your BVN to complete Tier 2 verification.", "system", []uuid.UUID{kyc.UserID})
			k.push.SendPushNotification(bgCtx, kyc.UserID, "NIN Verified", "Your NIN has been verified. Please verify your BVN to complete Tier 2 verification.")
		}()
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("NIN verified successfully", nil))
}

// func (k *KYC) uploadProofOfAddress(ctx *gin.Context) {
// 	// Address Information
// 	state := ctx.PostForm("state")
// 	lga := ctx.PostForm("lga")
// 	houseNumber := ctx.PostForm("house_number")
// 	streetName := ctx.PostForm("street_name")
// 	nearestLandmark := ctx.PostForm("nearest_landmark")
// 	postalCode := ctx.PostForm("postal_code")
// 	city := ctx.PostForm("city")

// 	if state == "" || lga == "" || houseNumber == "" || streetName == "" || postalCode == "" || city == "" {
// 		ctx.JSON(http.StatusBadRequest, basemodels.NewError("state, lga, house_number, street_name, and postal_code are required"))
// 		return
// 	}

// 	file, header, err := ctx.Request.FormFile("file")
// 	if err != nil {
// 		ctx.JSON(http.StatusBadRequest, basemodels.NewError("file is required"))
// 		return
// 	}
// 	defer file.Close()

// 	proofType := ctx.PostForm("proof_type")
// 	if proofType == "" {
// 		ctx.JSON(http.StatusBadRequest, basemodels.NewError("proof_type is required"))
// 		return
// 	}

// 	allowedProofTypes := []string{"utility_bill", "bank_statement", "tenancy_agreement"}
// 	normalizedProofType := strings.ToLower(strings.ReplaceAll(proofType, " ", "_"))

// 	isValidProofType := false
// 	for _, allowedType := range allowedProofTypes {
// 		if allowedType == normalizedProofType {
// 			isValidProofType = true
// 			break
// 		}
// 	}

// 	if !isValidProofType {
// 		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid proof_type. Must be one of: utility_bill, bank_statement, tenancy_agreement"))
// 		return
// 	}

// 	if header.Size > 15*1024*1024 {
// 		ctx.JSON(http.StatusRequestEntityTooLarge, basemodels.NewError("File size exceeds 15MB"))
// 		return
// 	}

// 	allowedContentTypes := []string{"image/png", "image/jpeg", "image/jpg", "application/pdf"}
// 	fileContentType := header.Header.Get("Content-Type")
// 	isValidContentType := false
// 	for _, allowedType := range allowedContentTypes {
// 		if fileContentType == allowedType {
// 			isValidContentType = true
// 			break
// 		}
// 	}

// 	if !isValidContentType {
// 		ctx.JSON(http.StatusBadRequest, basemodels.NewError("File type must be PNG, JPG, JPEG, or PDF"))
// 		return
// 	}

// 	imageData, err := io.ReadAll(file)
// 	if err != nil {
// 		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Error parsing file"))
// 		return
// 	}

// 	filename := ctx.DefaultPostForm("filename", fmt.Sprintf("%v_%v", proofType, time.Now().UTC().Format("20060102_150405")))

// 	activeUser, err := utils.GetActiveUser(ctx)
// 	if err != nil {
// 		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
// 		return
// 	}

// 	// Get KYC record
// 	userKyc, err := k.server.queries.GetKYCByUserID(ctx, activeUser.UserID)
// 	if err == sql.ErrNoRows {
// 		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Please complete basic KYC verification first"))
// 		return
// 	} else if err != nil {
// 		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
// 		return
// 	}

// 	// Update Address Information
// 	addressArgs := db.UpdateKYCAddressParams{
// 		ID: userKyc.ID,
// 		State: sql.NullString{
// 			String: utils.Encrypt(state, k.server.config.SigningKey),
// 			Valid:  true,
// 		},
// 		Lga: sql.NullString{
// 			String: utils.Encrypt(lga, k.server.config.SigningKey),
// 			Valid:  true,
// 		},
// 		HouseNumber: sql.NullString{
// 			String: utils.Encrypt(houseNumber, k.server.config.SigningKey),
// 			Valid:  true,
// 		},
// 		StreetName: sql.NullString{
// 			String: utils.Encrypt(streetName, k.server.config.SigningKey),
// 			Valid:  true,
// 		},
// 		NearestLandmark: sql.NullString{
// 			String: utils.Encrypt(nearestLandmark, k.server.config.SigningKey),
// 			Valid:  nearestLandmark != "",
// 		},
// 		PostalCode: sql.NullString{
// 			String: utils.Encrypt(postalCode, k.server.config.SigningKey),
// 			Valid:  postalCode != "",
// 		},
// 		City: sql.NullString{
// 			String: utils.Encrypt(city, k.server.config.SigningKey),
// 			Valid:  city != "",
// 		},
// 	}
// 	_, err = k.server.queries.UpdateKYCAddress(ctx, addressArgs)

// 	if err != nil {
// 		k.server.logger.Error(err)
// 		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Address update failed at DB level"))
// 		return
// 	}

// 	// Store proof document
// 	proof, err := k.server.queries.InsertNewProofImage(ctx, db.InsertNewProofImageParams{
// 		UserID:    activeUser.UserID,
// 		Filename:  filename,
// 		ProofType: proofType,
// 		ImageData: imageData,
// 	})
// 	if err != nil {
// 		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to upload proof of address: %s", err)))
// 		return
// 	}

// 	// Update KYC with proof information
// 	proofArgs := db.UpdateKYCProofOfAddressParams{
// 		ID: userKyc.ID,
// 		ProofOfAddressType: sql.NullString{
// 			String: utils.Encrypt(normalizedProofType, k.server.config.SigningKey),
// 			Valid:  true,
// 		},
// 		ProofOfAddressUrl: sql.NullString{
// 			String: utils.Encrypt(fmt.Sprintf("/api/v1/kyc/retrieve-address-proof/%d", proof.ID), k.server.config.SigningKey),
// 			Valid:  true,
// 		},
// 		ProofOfAddressDate: sql.NullTime{
// 			Time:  time.Now(),
// 			Valid: true,
// 		},
// 	}

// 	kyc, err := k.server.queries.UpdateKYCProofOfAddress(ctx, proofArgs)
// 	if err != nil {
// 		k.server.logger.Error(err)
// 		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to update KYC with proof information"))
// 		return
// 	}

// 	// Update to Tier 3
// 	_, err = k.server.queries.UpdateKYCToTierThree(ctx, kyc.ID)
// 	if err != nil {
// 		k.server.logger.Errorf("failed to update kyc %d to tier 3: %v", kyc.ID, err)
// 	}

// 	// Ensure status is verified and user table is updated
// 	_, err = k.server.queries.UpdateKYCStatus(ctx, db.UpdateKYCStatusParams{
// 		ID:     kyc.ID,
// 		Status: "verified",
// 	})
// 	if err != nil {
// 		k.server.logger.Errorf("failed to update kyc %d status to verified: %v", kyc.ID, err)
// 	}

// 	_, err = k.server.queries.UpdateUserKYCVerificationStatus(ctx, db.UpdateUserKYCVerificationStatusParams{
// 		ID:            kyc.UserID,
// 		IsKycVerified: true,
// 		UpdatedAt:     time.Now(),
// 	})
// 	if err != nil {
// 		k.server.logger.Errorf("failed to update user %d kyc verification status: %v", kyc.UserID, err)
// 	}

// 	// Refresh kyc object
// 	updatedKyc, err := k.server.queries.GetKYCByUserID(ctx, kyc.UserID)
// 	if err == nil {
// 		kyc = updatedKyc
// 	}

// 	// Send notifications
// 	go func() {
// 		bgCtx := context.Background()
// 		u, _ := k.server.queries.GetUserByID(bgCtx, kyc.UserID)
// 		k.email.KycVerified(bgCtx, u.FirstName.String, u.Email)
// 		k.notifyr.CreateWithRecipients(bgCtx, nil, "KYC Verified", "Your address verification (Tier 3) was successful.", "system", []uuid.UUID{kyc.UserID})
// 		k.push.SendPushNotification(bgCtx, kyc.UserID, "KYC Verified", "Your address verification (Tier 3) was successful.")
// 	}()

// 	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Proof of address uploaded and address updated successfully!", gin.H{
// 		"id":           models.ID(proof.ID),
// 		"user_id":      proof.UserID,
// 		"filename":     proof.Filename,
// 		"proof_type":   proof.ProofType,
// 		"created_at":   proof.CreatedAt,
// 		"kyc_status":   kyc.Status,
// 		"kyc_verified": kyc.Status == "verified",
// 	}))
// }

func (k *KYC) verifyUtilityBill(ctx *gin.Context) {
	// Address Information from Form
	state := ctx.PostForm("state")
	lga := ctx.PostForm("lga")
	houseNumber := ctx.PostForm("house_number")
	streetName := ctx.PostForm("street_name")
	nearestLandmark := ctx.PostForm("nearest_landmark")
	postalCode := ctx.PostForm("postal_code")
	city := ctx.PostForm("city")

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	userKYC, err := k.server.queries.GetKYCByUserID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("User KYC not found"))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	if userKYC.Tier != "tier_2" {
		ctx.JSON(http.StatusConflict, basemodels.NewError("You have to be on tier 2 to start tier 3 verifications"))
		return
	}

	dbUser, err := k.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	provider, exists := k.server.provider.GetProvider(providers.Dojah)
	if !exists {
		k.server.logger.Error("Dojah Provider not found")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	kycProvider, ok := provider.(*kyc.DOJAHProvider)
	if !ok {
		k.server.logger.Error("Cannot convert provider to DOJAHProvider")
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	file, header, err := ctx.Request.FormFile("file")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("file is required"))
		return
	}
	defer file.Close()

	if header.Size > 15*1024*1024 {
		ctx.JSON(http.StatusRequestEntityTooLarge, basemodels.NewError("File size exceeds 15MB"))
		return
	}

	allowedContentTypes := []string{"image/png", "image/jpeg", "image/jpg", "application/pdf"}
	fileContentType := header.Header.Get("Content-Type")
	isValidContentType := slices.Contains(allowedContentTypes, fileContentType)

	if !isValidContentType {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("File type must be PNG, JPG, JPEG, or PDF"))
		return
	}

	// Generate filename and save to assets/images
	ext := filepath.Ext(header.Filename)
	if ext == "" {
		switch fileContentType {
		case "image/png":
			ext = ".png"
		case "image/jpeg", "image/jpg":
			ext = ".jpg"
		case "application/pdf":
			ext = ".pdf"
		}
	}

	filename := fmt.Sprintf("utility_bill_%d_%d%s", time.Now().UnixNano(), activeUser.UserID, ext)
	filePath := filepath.Join("assets/images", filename)

	if err := ctx.SaveUploadedFile(header, filePath); err != nil {
		k.server.logger.Errorf("failed to save uploaded file: %v", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to save uploaded file"))
		return
	}
	// defer os.Remove(filePath)

	publicURL := fmt.Sprintf("https://swiftfiat-backend.swiftfiat.com/assets/images/%s", filename)

	k.server.logger.Info("Public URL: ", publicURL)
	// Call Dojah Utility Bill Analysis
	analysis, err := kycProvider.AnalyzeUtilityBill(publicURL, "url")
	if err != nil {
		k.server.logger.Errorf("Dojah utility bill analysis failed: %v", err)
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(fmt.Sprintf("Utility bill verification failed: %v", err)))
		return
	}

	if analysis.Result.Status != "success" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(fmt.Sprintf("Utility bill verification failed: %s", analysis.Result.Message)))
		return
	}

	// Get KYC record
	userKyc, err := k.server.queries.GetKYCByUserID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Please complete basic KYC verification first"))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Use analysis data if form fields are empty
	if state == "" {
		state = analysis.AddressInfo.State
	}
	if city == "" {
		city = analysis.AddressInfo.City
	}
	if streetName == "" {
		streetName = analysis.AddressInfo.Street
	}

	// Update Address Information
	addressArgs := db.UpdateKYCAddressParams{
		ID: userKyc.ID,
		State: sql.NullString{
			String: utils.Encrypt(state, k.server.config.SigningKey),
			Valid:  state != "",
		},
		Lga: sql.NullString{
			String: utils.Encrypt(lga, k.server.config.SigningKey),
			Valid:  lga != "",
		},
		HouseNumber: sql.NullString{
			String: utils.Encrypt(houseNumber, k.server.config.SigningKey),
			Valid:  houseNumber != "",
		},
		StreetName: sql.NullString{
			String: utils.Encrypt(streetName, k.server.config.SigningKey),
			Valid:  streetName != "",
		},
		NearestLandmark: sql.NullString{
			String: utils.Encrypt(nearestLandmark, k.server.config.SigningKey),
			Valid:  nearestLandmark != "",
		},
		PostalCode: sql.NullString{
			String: utils.Encrypt(postalCode, k.server.config.SigningKey),
			Valid:  postalCode != "",
		},
		City: sql.NullString{
			String: utils.Encrypt(city, k.server.config.SigningKey),
			Valid:  city != "",
		},
	}
	_, err = k.server.queries.UpdateKYCAddress(ctx, addressArgs)
	if err != nil {
		k.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Address update failed at DB level"))
		return
	}

	// Store proof document in DB as well
	file.Seek(0, 0)
	imageData, _ := io.ReadAll(file)

	var proofID int32
	proof, err := k.server.queries.InsertNewProofImage(ctx, db.InsertNewProofImageParams{
		UserID:    activeUser.UserID,
		Filename:  filename,
		ProofType: "utility_bill",
		ImageData: imageData,
	})
	if err == nil {
		proofID = proof.ID
	}

	// Update KYC with proof information
	proofUrl := publicURL
	if proofID > 0 {
		// Optionally we could use the internal retrieval URL here, but the user asked for public URL.
		// proofUrl = fmt.Sprintf("/api/v1/kyc/retrieve-address-proof/%d", proofID)
	}

	proofArgs := db.UpdateKYCProofOfAddressParams{
		ID: userKyc.ID,
		ProofOfAddressType: sql.NullString{
			String: utils.Encrypt("utility_bill", k.server.config.SigningKey),
			Valid:  true,
		},
		ProofOfAddressUrl: sql.NullString{
			String: utils.Encrypt(proofUrl, k.server.config.SigningKey),
			Valid:  proofUrl != "",
		},
		ProofOfAddressDate: sql.NullTime{
			Time:  time.Now(),
			Valid: true,
		},
	}

	updatedKycRecord, err := k.server.queries.UpdateKYCProofOfAddress(ctx, proofArgs)
	if err != nil {
		k.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to update KYC with proof information"))
		return
	}

	// Update to Tier 3
	_, err = k.server.queries.UpdateKYCToTierThree(ctx, updatedKycRecord.ID)
	if err != nil {
		k.server.logger.Errorf("failed to update kyc %d to tier 3: %v", updatedKycRecord.ID, err)
	}

	// Ensure status is verified and user table is updated
	_, err = k.server.queries.UpdateKYCStatus(ctx, db.UpdateKYCStatusParams{
		ID:     updatedKycRecord.ID,
		Status: "verified",
	})
	if err != nil {
		k.server.logger.Errorf("failed to update kyc %d status to verified: %v", updatedKycRecord.ID, err)
	}

	_, err = k.server.queries.UpdateUserKYCVerificationStatus(ctx, db.UpdateUserKYCVerificationStatusParams{
		ID:            updatedKycRecord.UserID,
		IsKycVerified: true,
		UpdatedAt:     time.Now(),
	})
	if err != nil {
		k.server.logger.Errorf("failed to update user %d kyc verification status: %v", updatedKycRecord.UserID, err)
	}

	// Refresh kyc object
	finalKyc, err := k.server.queries.GetKYCByUserID(ctx, updatedKycRecord.UserID)
	if err == nil {
		updatedKycRecord = finalKyc
	}

	// Send notifications
	go func() {
		bgCtx := context.Background()
		k.email.KycVerified(bgCtx, dbUser.FirstName.String, dbUser.Email)
		k.notifyr.CreateWithRecipients(bgCtx, nil, "KYC Verified", "Your address verification (Tier 3) was successful.", "system", []uuid.UUID{updatedKycRecord.UserID})
		k.push.SendPushNotification(bgCtx, updatedKycRecord.UserID, "KYC Verified", "Your address verification (Tier 3) was successful.")
	}()

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Utility bill verified and address updated successfully!", gin.H{
		"kyc_status":   updatedKycRecord.Status,
		"kyc_verified": updatedKycRecord.Status == "verified",
		"analysis":     analysis,
		"image_url":    publicURL,
	}))
}

func (k *KYC) retrieveProofOfAddress(c *gin.Context) {
	id := c.Param("id")
	idObj, err := models.ParseIDFromString(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}

	proof, err := k.server.queries.GetProofImage(c, int32(idObj))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Proof of address not found"})
		return
	}

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", proof.Filename))
	c.Data(http.StatusOK, "application/octet-stream", proof.ImageData)
}

func (k *KYC) verifyKYC(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	id := ctx.Param("id")
	idObj, err := models.ParseIDFromString(id)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid KYC ID"))
		return
	}

	kyc, err := k.server.queries.ManuallyVerifyKYC(ctx, int64(idObj))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to verify KYC"))
		return
	}

	// Update user table
	_, err = k.server.queries.UpdateUserKYCVerificationStatus(ctx, db.UpdateUserKYCVerificationStatusParams{
		ID:            kyc.UserID,
		IsKycVerified: true,
		UpdatedAt:     time.Now(),
	})
	if err != nil {
		k.server.logger.Errorf("failed to update user %d kyc verification status: %v", kyc.UserID, err)
	}

	// Send notifications
	go func() {
		bgCtx := context.Background()
		u, _ := k.server.queries.GetUserByID(bgCtx, kyc.UserID)
		k.email.KycVerified(bgCtx, u.FirstName.String, u.Email)
		k.notifyr.CreateWithRecipients(bgCtx, nil, "KYC Verified", "Your KYC has been manually verified by an administrator.", "system", []uuid.UUID{kyc.UserID})
		k.push.SendPushNotification(bgCtx, kyc.UserID, "KYC Verified", "Your KYC has been manually verified by an administrator.")
	}()

	// Decrypt KYC data
	k.decryptKyc(&kyc)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("KYC verified successfully", models.ToUserKYCInformation(&kyc)))
}

func (k *KYC) rejectKYC(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusForbidden, basemodels.NewError(apistrings.UnauthorizedAccess))
		return
	}

	id := ctx.Param("id")
	idObj, err := models.ParseIDFromString(id)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid KYC ID"))
		return
	}

	var req struct {
		Reason string `json:"reason" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Reason is required for rejection"))
		return
	}

	kyc, err := k.server.queries.RejectKYC(ctx, db.RejectKYCParams{
		ID:      int64(idObj),
		Column2: req.Reason,
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to reject KYC"))
		return
	}

	// Update user table
	_, err = k.server.queries.UpdateUserKYCVerificationStatus(ctx, db.UpdateUserKYCVerificationStatusParams{
		ID:            kyc.UserID,
		IsKycVerified: false,
		UpdatedAt:     time.Now(),
	})
	if err != nil {
		k.server.logger.Errorf("failed to update user %d kyc verification status: %v", kyc.UserID, err)
	}

	// Send notifications
	go func() {
		bgCtx := context.Background()
		u, _ := k.server.queries.GetUserByID(bgCtx, kyc.UserID)
		k.email.KycFailed(bgCtx, u.FirstName.String, u.Email, req.Reason)
		k.notifyr.CreateWithRecipients(bgCtx, nil, "KYC Rejected", fmt.Sprintf("Your KYC was rejected. Reason: %s", req.Reason), "system", []uuid.UUID{kyc.UserID})
		k.push.SendPushNotification(bgCtx, kyc.UserID, "KYC Rejected", fmt.Sprintf("Your KYC was rejected. Reason: %s", req.Reason))
	}()

	// Decrypt KYC data
	k.decryptKyc(&kyc)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("KYC rejected successfully", models.ToUserKYCInformation(&kyc)))
}

func (k *KYC) getAdminUserKyc(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("Unauthorized"))
		return
	}

	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid User ID"))
		return
	}

	// Get KYC by User ID
	userKyc, err := k.server.queries.GetKYCByUserID(ctx, id)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusNotFound, basemodels.NewError("KYC record not found for this user"))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Now get the extended info using the KYC ID
	extendedKyc, err := k.server.queries.GetUserAndKYCWithProofOfAddress(ctx, userKyc.ID)
	if err != nil {
		// Decrypt basic info
		k.decryptKyc(&userKyc)
		// Fallback to basic info if extended info fails
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("User KYC Information Fetched", models.ToUserKYCInformation(&userKyc)))
		return
	}

	// Decrypt extended info
	k.decryptExtendedKyc(&extendedKyc)

	entry := audit.NewLog(
		ctx,
		audit.CategoryKYC,
		audit.EventGetKYC,
		fmt.Sprintf("User ID: %d", id),
		fmt.Sprintf("admin %s viewed kyc of user with id %d", activeUser.Email, id),
		&activeUser.UserID,
		activeUser.Role,
		true,
		nil,
	)
	k.audit.Log(entry)

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("User KYC Information Fetched", models.ToUserKYCInformationExtended(&extendedKyc)))
}

func (k *KYC) decryptField(field string) string {
	if field == "" {
		return ""
	}
	decrypted := utils.Decrypt(field, k.server.config.SigningKey)
	if decrypted == "" {
		// If decryption fails, it might be unencrypted data or a different error.
		// We return the original field to maintain compatibility with existing data.
		return field
	}
	return decrypted
}

func (k *KYC) decryptKyc(kyc *db.Kyc) {
	if kyc.FullName.Valid {
		kyc.FullName.String = k.decryptField(kyc.FullName.String)
	}
	if kyc.PhoneNumber.Valid {
		kyc.PhoneNumber.String = k.decryptField(kyc.PhoneNumber.String)
	}
	if kyc.Email.Valid {
		kyc.Email.String = k.decryptField(kyc.Email.String)
	}
	if kyc.Gender.Valid {
		kyc.Gender.String = k.decryptField(kyc.Gender.String)
	}
	if kyc.SelfieUrl.Valid {
		kyc.SelfieUrl.String = k.decryptField(kyc.SelfieUrl.String)
	}
	if kyc.Bvn.Valid {
		kyc.Bvn.String = k.decryptField(kyc.Bvn.String)
	}
	if kyc.Nin.Valid {
		kyc.Nin.String = k.decryptField(kyc.Nin.String)
	}
	if kyc.IDType.Valid {
		kyc.IDType.String = k.decryptField(kyc.IDType.String)
	}
	if kyc.IDNumber.Valid {
		kyc.IDNumber.String = k.decryptField(kyc.IDNumber.String)
	}
	if kyc.IDImageUrl.Valid {
		kyc.IDImageUrl.String = k.decryptField(kyc.IDImageUrl.String)
	}
	if kyc.State.Valid {
		kyc.State.String = k.decryptField(kyc.State.String)
	}
	if kyc.Lga.Valid {
		kyc.Lga.String = k.decryptField(kyc.Lga.String)
	}
	if kyc.HouseNumber.Valid {
		kyc.HouseNumber.String = k.decryptField(kyc.HouseNumber.String)
	}
	if kyc.StreetName.Valid {
		kyc.StreetName.String = k.decryptField(kyc.StreetName.String)
	}
	if kyc.NearestLandmark.Valid {
		kyc.NearestLandmark.String = k.decryptField(kyc.NearestLandmark.String)
	}
	if kyc.PostalCode.Valid {
		kyc.PostalCode.String = k.decryptField(kyc.PostalCode.String)
	}
	if kyc.Country.Valid {
		kyc.Country.String = k.decryptField(kyc.Country.String)
	}
	if kyc.ProofOfAddressType.Valid {
		kyc.ProofOfAddressType.String = k.decryptField(kyc.ProofOfAddressType.String)
	}
	if kyc.ProofOfAddressUrl.Valid {
		kyc.ProofOfAddressUrl.String = k.decryptField(kyc.ProofOfAddressUrl.String)
	}
}

func (k *KYC) decryptExtendedKyc(kyc *db.GetUserAndKYCWithProofOfAddressRow) {
	if kyc.FullName.Valid {
		kyc.FullName.String = k.decryptField(kyc.FullName.String)
	}
	if kyc.PhoneNumber.Valid {
		kyc.PhoneNumber.String = k.decryptField(kyc.PhoneNumber.String)
	}
	if kyc.Email.Valid {
		kyc.Email.String = k.decryptField(kyc.Email.String)
	}
	if kyc.Gender.Valid {
		kyc.Gender.String = k.decryptField(kyc.Gender.String)
	}
	if kyc.SelfieUrl.Valid {
		kyc.SelfieUrl.String = k.decryptField(kyc.SelfieUrl.String)
	}
	if kyc.Bvn.Valid {
		kyc.Bvn.String = k.decryptField(kyc.Bvn.String)
	}
	if kyc.Nin.Valid {
		kyc.Nin.String = k.decryptField(kyc.Nin.String)
	}
	if kyc.IDType.Valid {
		kyc.IDType.String = k.decryptField(kyc.IDType.String)
	}
	if kyc.IDNumber.Valid {
		kyc.IDNumber.String = k.decryptField(kyc.IDNumber.String)
	}
	if kyc.IDImageUrl.Valid {
		kyc.IDImageUrl.String = k.decryptField(kyc.IDImageUrl.String)
	}
	if kyc.State.Valid {
		kyc.State.String = k.decryptField(kyc.State.String)
	}
	if kyc.Lga.Valid {
		kyc.Lga.String = k.decryptField(kyc.Lga.String)
	}
	if kyc.HouseNumber.Valid {
		kyc.HouseNumber.String = k.decryptField(kyc.HouseNumber.String)
	}
	if kyc.StreetName.Valid {
		kyc.StreetName.String = k.decryptField(kyc.StreetName.String)
	}
	if kyc.NearestLandmark.Valid {
		kyc.NearestLandmark.String = k.decryptField(kyc.NearestLandmark.String)
	}
	if kyc.PostalCode.Valid {
		kyc.PostalCode.String = k.decryptField(kyc.PostalCode.String)
	}
	if kyc.Country.Valid {
		kyc.Country.String = k.decryptField(kyc.Country.String)
	}
	if kyc.ProofOfAddressType.Valid {
		kyc.ProofOfAddressType.String = k.decryptField(kyc.ProofOfAddressType.String)
	}
	if kyc.ProofOfAddressUrl.Valid {
		kyc.ProofOfAddressUrl.String = k.decryptField(kyc.ProofOfAddressUrl.String)
	}
}

func (k *KYC) listAllUserKyc(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}
	if activeUser.Role == models.USER {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("Unauthorized"))
		return
	}

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	offset := (page - 1) * limit

	kycs, err := k.server.queries.ListAllKYC(ctx, db.ListAllKYCParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		k.server.logger.Errorf("failed to list all kyc: %v", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	stats, err := k.server.queries.GetKYCStatistics(ctx)
	if err != nil {
		k.server.logger.Errorf("failed to get kyc statistics: %v", err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	var response []models.UserKYCInformation
	for _, kycRow := range kycs {
		// Create a temporary Kyc object for decryption
		tempKyc := db.Kyc{
			FullName:           kycRow.FullName,
			PhoneNumber:        kycRow.PhoneNumber,
			Email:              kycRow.Email,
			Gender:             kycRow.Gender,
			SelfieUrl:          kycRow.SelfieUrl,
			Bvn:                kycRow.Bvn,
			Nin:                kycRow.Nin,
			IDType:             kycRow.IDType,
			IDNumber:           kycRow.IDNumber,
			IDImageUrl:         kycRow.IDImageUrl,
			State:              kycRow.State,
			Lga:                kycRow.Lga,
			HouseNumber:        kycRow.HouseNumber,
			StreetName:         kycRow.StreetName,
			NearestLandmark:    kycRow.NearestLandmark,
			PostalCode:         kycRow.PostalCode,
			Country:            kycRow.Country,
			ProofOfAddressType: kycRow.ProofOfAddressType,
			ProofOfAddressUrl:  kycRow.ProofOfAddressUrl,
		}
		k.decryptKyc(&tempKyc)

		// Update kycRow with decrypted values
		kycRow.FullName = tempKyc.FullName
		kycRow.PhoneNumber = tempKyc.PhoneNumber
		kycRow.Email = tempKyc.Email
		kycRow.Gender = tempKyc.Gender
		kycRow.SelfieUrl = tempKyc.SelfieUrl
		kycRow.Bvn = tempKyc.Bvn
		kycRow.Nin = tempKyc.Nin
		kycRow.IDType = tempKyc.IDType
		kycRow.IDNumber = tempKyc.IDNumber
		kycRow.IDImageUrl = tempKyc.IDImageUrl
		kycRow.State = tempKyc.State
		kycRow.Lga = tempKyc.Lga
		kycRow.HouseNumber = tempKyc.HouseNumber
		kycRow.StreetName = tempKyc.StreetName
		kycRow.NearestLandmark = tempKyc.NearestLandmark
		kycRow.PostalCode = tempKyc.PostalCode
		kycRow.Country = tempKyc.Country
		kycRow.ProofOfAddressType = tempKyc.ProofOfAddressType
		kycRow.ProofOfAddressUrl = tempKyc.ProofOfAddressUrl

		response = append(response, *models.ToListAllKYCInformation(&kycRow))
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("All User KYC Information Fetched", gin.H{
		"kycs":        response,
		"total_count": stats.TotalCount,
		"page":        page,
		"limit":       limit,
	}))
}
