package api

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers"
	"github.com/SwiftFiat/SwiftFiat-Backend/providers/kyc"
	service "github.com/SwiftFiat/SwiftFiat-Backend/services/notification"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type KYC struct {
	server  *Server
	notifyr *service.Notification
	push    *service.PushNotificationService
	email   *service.Plunk
}

func (k KYC) router(server *Server) {
	k.server = server
	k.notifyr = service.NewNotificationService(k.server.queries)
	k.push = server.pushNotification
	k.email = server.emailService

	serverGroupV1 := server.router.Group("/api/v1/kyc")
	serverGroupV1.GET("", k.server.authMiddleware.AuthenticatedMiddleware(), k.getUserKyc)
	serverGroupV1.POST("validate-bvn", k.server.authMiddleware.AuthenticatedMiddleware(), k.validateBVN)
	serverGroupV1.POST("validate-nin", k.server.authMiddleware.AuthenticatedMiddleware(), k.validateNIN)
	serverGroupV1.POST("upload-address-proof", k.server.authMiddleware.AuthenticatedMiddleware(), k.uploadProofOfAddress)
	serverGroupV1.GET("retrieve-address-proof/:id", k.server.authMiddleware.AuthenticatedMiddleware(), k.retrieveProofOfAddress)

	// New endpoint to check verification progress
	serverGroupV1.GET("verification-progress", k.server.authMiddleware.AuthenticatedMiddleware(), k.getVerificationProgress)

	// Admin endpoints
	adminGroup := server.router.Group("/api/v1/admin/kyc")
	adminGroup.Use(k.server.authMiddleware.AuthenticatedMiddleware())
	{
		adminGroup.POST("/verify/:id", k.verifyKYC)
		adminGroup.POST("/reject/:id", k.rejectKYC)
	}
}

// getVerificationProgress returns which fields are completed and what's still needed
func (k *KYC) getVerificationProgress(ctx *gin.Context) {
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	userKyc, err := k.server.queries.GetKYCByUserID(ctx, int32(activeUser.UserID))
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

	userKyc, err := k.server.queries.GetKYCByUserID(ctx, int32(activeUser.UserID))
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.UserNoKYC))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("User KYC Information Fetched Successfully", gin.H{
		"status":  userKyc.Status,
		"tier":    userKyc.Tier,
		"date":    userKyc.VerificationDate.Time,
		"user_id": userKyc.UserID,
	}))
}

func (k *KYC) validateBVN(ctx *gin.Context) {
	request := struct {
		BVN    string `json:"bvn" binding:"required"`
		Gender string `json:"gender"`
		DOB    string `json:"dob"`
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

	dbUser, err := k.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	if !dbUser.Verified {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("you have not verified your account yet"))
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

	verificationData, err := kycProvider.ValidateBVN(request.BVN, dbUser.FirstName.String, dbUser.LastName.String, &request.DOB)
	if err != nil {
		k.server.logger.Error(err)
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(fmt.Sprintf("BVN Validation Failure: %s", err)))
		return
	}

	k.server.logger.Log(logrus.InfoLevel, "Verification Data: ", verificationData)

	if !verificationData.FirstName.Status {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Provided FirstName does not match First Name on BVN"))
		return
	}

	if !verificationData.LastName.Status {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Provided LastName does not match Last Name on BVN"))
		return
	}

	if !verificationData.DOB.Status {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Provided DOB does not match DOB on BVN"))
		return
	}

	// Get or create KYC record
	userKyc, err := k.server.queries.GetKYCByUserID(ctx, int32(activeUser.UserID))
	if err == sql.ErrNoRows {
		userKyc, err = k.server.queries.CreateNewKYC(ctx, int32(activeUser.UserID))
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
			String: verificationData.BVN.Value,
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
			ID:            int64(kyc.UserID),
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
			k.notifyr.CreateWithRecipients(bgCtx, nil, "KYC Verified", "Your identity verification (Tier 2) was successful.", "system", []int64{int64(kyc.UserID)})
			k.push.SendPushNotification(bgCtx, int64(kyc.UserID), "KYC Verified", "Your identity verification (Tier 2) was successful.")
			k.email.KycVerified(bgCtx, dbUser.FirstName.String, dbUser.Email)
		}()
	} else {
		// Only BVN verified
		go func() {
			bgCtx := context.Background()
			k.notifyr.CreateWithRecipients(bgCtx, nil, "BVN Verified", "Your BVN has been verified. Please verify your NIN to complete Tier 2 verification.", "system", []int64{int64(kyc.UserID)})
			k.push.SendPushNotification(bgCtx, int64(kyc.UserID), "BVN Verified", "Your BVN has been verified. Please verify your NIN to complete Tier 2 verification.")
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

	dbUser, err := k.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	if !dbUser.Verified {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("you have not verified your account yet"))
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

	ninRequest := map[string]interface{}{
		"nin":    request.NIN,
		"selfie": request.Selfie,
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

	// Get or create KYC record
	userKyc, err := k.server.queries.GetKYCByUserID(ctx, int32(activeUser.UserID))
	if err == sql.ErrNoRows {
		userKyc, err = k.server.queries.CreateNewKYC(ctx, int32(activeUser.UserID))
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
			String: verificationData.NIN,
			Valid:  true,
		},
		Gender: sql.NullString{
			String: strings.ToLower(verificationData.Gender),
			Valid:  true,
		},
		SelfieUrl: sql.NullString{
			String: verificationData.Image,
			Valid:  true,
		},
		PhoneNumber: sql.NullString{
			String: verificationData.PhoneNumber,
			Valid:  true,
		},
		FullName: sql.NullString{
			String: verificationData.FirstName + " " + verificationData.LastName,
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
			ID:            int64(kyc.UserID),
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
			k.notifyr.CreateWithRecipients(bgCtx, nil, "KYC Verified", "Your identity verification (Tier 2) was successful.", "system", []int64{int64(kyc.UserID)})
			k.push.SendPushNotification(bgCtx, int64(kyc.UserID), "KYC Verified", "Your identity verification (Tier 2) was successful.")
			k.email.KycVerified(bgCtx, dbUser.FirstName.String, dbUser.Email)
		}()
	} else {
		// Only NIN verified
		go func() {
			bgCtx := context.Background()
			k.notifyr.CreateWithRecipients(bgCtx, nil, "NIN Verified", "Your NIN has been verified. Please verify your BVN to complete Tier 2 verification.", "system", []int64{int64(kyc.UserID)})
			k.push.SendPushNotification(bgCtx, int64(kyc.UserID), "NIN Verified", "Your NIN has been verified. Please verify your BVN to complete Tier 2 verification.")
		}()
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("NIN verified successfully", nil))
}

func (k *KYC) uploadProofOfAddress(ctx *gin.Context) {
	// Address Information
	state := ctx.PostForm("state")
	lga := ctx.PostForm("lga")
	houseNumber := ctx.PostForm("house_number")
	streetName := ctx.PostForm("street_name")
	nearestLandmark := ctx.PostForm("nearest_landmark")

	if state == "" || lga == "" || houseNumber == "" || streetName == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("state, lga, house_number, and street_name are required"))
		return
	}

	file, header, err := ctx.Request.FormFile("file")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("file is required"))
		return
	}
	defer file.Close()

	proofType := ctx.PostForm("proof_type")
	if proofType == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("proof_type is required"))
		return
	}

	allowedProofTypes := []string{"utility_bill", "bank_statement", "tenancy_agreement"}
	normalizedProofType := strings.ToLower(strings.ReplaceAll(proofType, " ", "_"))

	isValidProofType := false
	for _, allowedType := range allowedProofTypes {
		if allowedType == normalizedProofType {
			isValidProofType = true
			break
		}
	}

	if !isValidProofType {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Invalid proof_type. Must be one of: utility_bill, bank_statement, tenancy_agreement"))
		return
	}

	if header.Size > 15*1024*1024 {
		ctx.JSON(http.StatusRequestEntityTooLarge, basemodels.NewError("File size exceeds 15MB"))
		return
	}

	allowedContentTypes := []string{"image/png", "image/jpeg", "image/jpg", "application/pdf"}
	fileContentType := header.Header.Get("Content-Type")
	isValidContentType := false
	for _, allowedType := range allowedContentTypes {
		if fileContentType == allowedType {
			isValidContentType = true
			break
		}
	}

	if !isValidContentType {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("File type must be PNG, JPG, JPEG, or PDF"))
		return
	}

	imageData, err := io.ReadAll(file)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Error parsing file"))
		return
	}

	filename := ctx.DefaultPostForm("filename", fmt.Sprintf("%v_%v", proofType, time.Now().UTC().Format("20060102_150405")))

	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	if !activeUser.Verified {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("you have not verified your account yet"))
		return
	}

	// Get KYC record
	userKyc, err := k.server.queries.GetKYCByUserID(ctx, int32(activeUser.UserID))
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("Please complete basic KYC verification first"))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	// Update Address Information
	addressArgs := db.UpdateKYCAddressParams{
		ID: userKyc.ID,
		State: sql.NullString{
			String: state,
			Valid:  true,
		},
		Lga: sql.NullString{
			String: lga,
			Valid:  true,
		},
		HouseNumber: sql.NullString{
			String: houseNumber,
			Valid:  true,
		},
		StreetName: sql.NullString{
			String: streetName,
			Valid:  true,
		},
		NearestLandmark: sql.NullString{
			String: nearestLandmark,
			Valid:  nearestLandmark != "",
		},
	}

	_, err = k.server.queries.UpdateKYCAddress(ctx, addressArgs)
	if err != nil {
		k.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Address update failed at DB level"))
		return
	}

	// Store proof document
	proof, err := k.server.queries.InsertNewProofImage(ctx, db.InsertNewProofImageParams{
		UserID:    int32(activeUser.UserID),
		Filename:  filename,
		ProofType: proofType,
		ImageData: imageData,
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to upload proof of address: %s", err)))
		return
	}

	// Update KYC with proof information
	proofArgs := db.UpdateKYCProofOfAddressParams{
		ID: userKyc.ID,
		ProofOfAddressType: sql.NullString{
			String: normalizedProofType,
			Valid:  true,
		},
		ProofOfAddressUrl: sql.NullString{
			String: fmt.Sprintf("/api/v1/kyc/retrieve-address-proof/%d", proof.ID),
			Valid:  true,
		},
		ProofOfAddressDate: sql.NullTime{
			Time:  time.Now(),
			Valid: true,
		},
	}

	kyc, err := k.server.queries.UpdateKYCProofOfAddress(ctx, proofArgs)
	if err != nil {
		k.server.logger.Error(err)
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Failed to update KYC with proof information"))
		return
	}

	// Update to Tier 3
	_, err = k.server.queries.UpdateKYCToTierThree(ctx, kyc.ID)
	if err != nil {
		k.server.logger.Errorf("failed to update kyc %d to tier 3: %v", kyc.ID, err)
	}

	// Ensure status is verified and user table is updated
	_, err = k.server.queries.UpdateKYCStatus(ctx, db.UpdateKYCStatusParams{
		ID:     kyc.ID,
		Status: "verified",
	})
	if err != nil {
		k.server.logger.Errorf("failed to update kyc %d status to verified: %v", kyc.ID, err)
	}

	_, err = k.server.queries.UpdateUserKYCVerificationStatus(ctx, db.UpdateUserKYCVerificationStatusParams{
		ID:            int64(kyc.UserID),
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
		u, _ := k.server.queries.GetUserByID(bgCtx, int64(kyc.UserID))
		k.email.KycVerified(bgCtx, u.FirstName.String, u.Email)
		k.notifyr.CreateWithRecipients(bgCtx, nil, "KYC Verified", "Your address verification (Tier 3) was successful.", "system", []int64{int64(kyc.UserID)})
		k.push.SendPushNotification(bgCtx, int64(kyc.UserID), "KYC Verified", "Your address verification (Tier 3) was successful.")
	}()

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Proof of address uploaded and address updated successfully!", gin.H{
		"id":           models.ID(proof.ID),
		"user_id":      models.ID(proof.UserID),
		"filename":     proof.Filename,
		"proof_type":   proof.ProofType,
		"created_at":   proof.CreatedAt,
		"kyc_status":   kyc.Status,
		"kyc_verified": kyc.Status == "verified",
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
		ID:            int64(kyc.UserID),
		IsKycVerified: true,
		UpdatedAt:     time.Now(),
	})
	if err != nil {
		k.server.logger.Errorf("failed to update user %d kyc verification status: %v", kyc.UserID, err)
	}

	// Send notifications
	go func() {
		bgCtx := context.Background()
		u, _ := k.server.queries.GetUserByID(bgCtx, int64(kyc.UserID))
		k.email.KycVerified(bgCtx, u.FirstName.String, u.Email)
		k.notifyr.CreateWithRecipients(bgCtx, nil, "KYC Verified", "Your KYC has been manually verified by an administrator.", "system", []int64{int64(kyc.UserID)})
		k.push.SendPushNotification(bgCtx, int64(kyc.UserID), "KYC Verified", "Your KYC has been manually verified by an administrator.")
	}()

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
		ID:            int64(kyc.UserID),
		IsKycVerified: false,
		UpdatedAt:     time.Now(),
	})
	if err != nil {
		k.server.logger.Errorf("failed to update user %d kyc verification status: %v", kyc.UserID, err)
	}

	// Send notifications
	go func() {
		bgCtx := context.Background()
		u, _ := k.server.queries.GetUserByID(bgCtx, int64(kyc.UserID))
		k.email.KycFailed(bgCtx, u.FirstName.String, u.Email, req.Reason)
		k.notifyr.CreateWithRecipients(bgCtx, nil, "KYC Rejected", fmt.Sprintf("Your KYC was rejected. Reason: %s", req.Reason), "system", []int64{int64(kyc.UserID)})
		k.push.SendPushNotification(bgCtx, int64(kyc.UserID), "KYC Rejected", fmt.Sprintf("Your KYC was rejected. Reason: %s", req.Reason))
	}()

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("KYC rejected successfully", models.ToUserKYCInformation(&kyc)))
}
