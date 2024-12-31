package api

import (
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
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type KYC struct {
	server *Server
}

func (k KYC) router(server *Server) {
	k.server = server

	// serverGroupV1 := server.router.Group("/auth")
	serverGroupV1 := server.router.Group("/api/v1/kyc")
	serverGroupV1.GET("", AuthenticatedMiddleware(), k.getUserKyc)
	serverGroupV1.POST("validate-bvn", AuthenticatedMiddleware(), k.validateBVN)
	serverGroupV1.POST("validate-nin", AuthenticatedMiddleware(), k.validateNIN)
	serverGroupV1.POST("update-address", AuthenticatedMiddleware(), k.updateAddress)
	serverGroupV1.POST("submit-utility", AuthenticatedMiddleware(), k.submitUtility)
	serverGroupV1.POST("upload-address-proof", AuthenticatedMiddleware(), k.uploadProofOfAddress)
	serverGroupV1.GET("retrieve-address-proof/:id", AuthenticatedMiddleware(), k.retrieveProofOfAddress)
}

func (k *KYC) getUserKyc(ctx *gin.Context) {
	// Fetch user details
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

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("User KYC Information Fetched Successfully", models.ToUserKYCInformation(&userKyc)))
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

	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	/// Check if user exists
	dbUser, err := k.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	/// check varification status
	if !dbUser.Verified {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("you have not verified your account yet"))
		return
	}

	if provider, exists := k.server.provider.GetProvider(providers.Dojah); exists {
		kycProvider, ok := provider.(*kyc.DOJAHProvider)
		if ok {
			verificationData, err := kycProvider.ValidateBVN(request.BVN, dbUser.FirstName.String, dbUser.LastName.String, nil)
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to connect to KYC Provider Error: %s", err)))
				return
			}
			/// Log Verification DATA
			k.server.logger.Log(logrus.InfoLevel, "Verification Data: ", verificationData)

			/// FirstName does not match First Name on BVN
			if !verificationData.FirstName.Status {
				ctx.JSON(http.StatusBadRequest, basemodels.NewError("Provided FirstName does not match First Name on BVN"))
				return
			}

			/// LastName does not match Last Name on BVN
			if !verificationData.LastName.Status {
				ctx.JSON(http.StatusBadRequest, basemodels.NewError("Provided LastName does not match Last Name on BVN"))
				return
			}

			/// DOB does not match DOB on BVN
			/// TODO: SKIP the DOB for now
			// if !verificationData.DOB.Status {
			// 	ctx.JSON(http.StatusBadRequest, basemodels.NewError("Provided DOB does not match DOB on BVN"))
			// 	return
			// }

			/// check verification data status
			if verificationData.FirstName.Status || verificationData.LastName.Status || verificationData.DOB.Status {

				/// Check for User's KYC file or create one if it doesn't exist
				userKyc, err := k.server.queries.GetKYCByUserID(ctx, int32(activeUser.UserID))
				if err == sql.ErrNoRows {
					userKyc, err = k.server.queries.CreateNewKYC(ctx, db.CreateNewKYCParams{
						UserID: int32(activeUser.UserID),
						Tier:   0,
					})
					if err == sql.ErrNoRows {
						ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
						return
					} else if err != nil {
						ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
						return
					}
				} else if err != nil {
					ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
					return
				}

				// Determine gender first
				/// Default user's gender to male unless explicitly specified
				/// Suggested by Joel SwiftFiat => 06/Oct/'24 - 5:41pm
				genderString := "male"
				if request.Gender != "" {
					genderString = request.Gender
				}

				args := db.UpdateKYCLevel1Params{
					ID: userKyc.ID,
					FullName: sql.NullString{
						String: dbUser.FirstName.String + " " + dbUser.LastName.String,
						Valid:  dbUser.FirstName.Valid && dbUser.LastName.Valid,
					},
					PhoneNumber: sql.NullString{
						String: dbUser.PhoneNumber,
						Valid:  true,
					},
					Email: sql.NullString{
						String: dbUser.Email,
						Valid:  true,
					},
					Bvn: sql.NullString{
						String: verificationData.BVN,
						Valid:  verificationData.BVN != "",
					},
					SelfieUrl: sql.NullString{
						String: "https://www.example.com",
						Valid:  true,
					},
					Gender: sql.NullString{
						String: genderString,
						Valid:  true,
					},
				}

				tx, err := k.server.queries.DB.Begin()
				if err != nil {
					panic(err)
				}
				defer tx.Rollback()

				kyc, err := k.server.queries.WithTx(tx).UpdateKYCLevel1(ctx, args)
				if err != nil {
					ctx.JSON(http.StatusInternalServerError, basemodels.NewError("KYC Validation error occurred at the DB Level"))
					return
				}

				err = tx.Commit()
				if err != nil {
					ctx.JSON(http.StatusInternalServerError, basemodels.NewError("KYC Validation error occurred at the DB Level"))
					return
				}

				ctx.JSON(http.StatusOK, basemodels.NewSuccess("BVN Success", models.ToUserKYCInformation(&kyc)))
				return
			}

			ctx.JSON(http.StatusOK, basemodels.NewError("BVN Validation Failure, please try again later"))
			return
		}
	}

	ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
}

func (k *KYC) validateNIN(ctx *gin.Context) {
	request := struct {
		NIN string `json:"nin" binding:"required"`
		// Selfie    string `json:"selfie_image" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		k.server.logger.Log(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidNINInput))
		return
	}

	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	/// Check if user exists
	dbUser, err := k.server.queries.GetUserByID(ctx, activeUser.UserID)
	if err == sql.ErrNoRows {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	/// check varification status
	if !dbUser.Verified {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("you have not verified your account yet"))
		return
	}

	if provider, exists := k.server.provider.GetProvider(providers.Dojah); exists {
		kycProvider, ok := provider.(*kyc.DOJAHProvider)
		if ok {
			verificationData, err := kycProvider.ValidateNIN(request)
			if err != nil {
				ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to connect to KYC Provider Error: %s", err)))
				return
			}
			/// Log Verification DATA
			k.server.logger.Log(logrus.InfoLevel, "Verification Data: ", verificationData)

			/// FirstName does not match First Name on BVN
			if verificationData.FirstName == "" {
				ctx.JSON(http.StatusBadRequest, basemodels.NewError("Provided FirstName does not match First Name on NIN"))
				return
			}

			/// LastName does not match Last Name on BVN
			if verificationData.LastName == "" {
				ctx.JSON(http.StatusBadRequest, basemodels.NewError("Provided LastName does not match Last Name on NIN"))
				return
			}

			/// LastName does not match Last Name on BVN
			if !verificationData.SelfieVerification.Match {
				ctx.JSON(http.StatusBadRequest, basemodels.NewError("Provided Image does not match Image on NIN"))
				return
			}

			/// DOB does not match DOB on BVN
			/// TODO: SKIP the DOB for now
			// if !verificationData.DOB.Status {
			// 	ctx.JSON(http.StatusBadRequest, basemodels.NewError("Provided DOB does not match DOB on BVN"))
			// 	return
			// }

			/// check verification data status
			if verificationData.SelfieVerification.Match {

				/// Check for User's KYC file or create one if it doesn't exist
				userKyc, err := k.server.queries.GetKYCByUserID(ctx, int32(activeUser.UserID))
				if err == sql.ErrNoRows {
					userKyc, err = k.server.queries.CreateNewKYC(ctx, db.CreateNewKYCParams{
						UserID: int32(activeUser.UserID),
						Tier:   0,
					})
					if err == sql.ErrNoRows {
						ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
						return
					} else if err != nil {
						ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
						return
					}
				} else if err != nil {
					ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
					return
				}

				args := db.UpdateKYCNINParams{
					ID: userKyc.ID,
					Nin: sql.NullString{
						String: verificationData.NIN,
						Valid:  verificationData.NIN != "",
					},
				}
				kyc, err := k.server.queries.UpdateKYCNIN(ctx, args)
				if err != nil {
					ctx.JSON(http.StatusInternalServerError, basemodels.NewError("KYC Validation error occurred at the DB Level"))
					return
				}
				ctx.JSON(http.StatusOK, basemodels.NewSuccess("NIN Success", models.ToUserKYCInformation(&kyc)))
				return
			}

			ctx.JSON(http.StatusOK, basemodels.NewError("NIN Validation Failure, please try again later"))
			return
		}
	}

	ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
}

func (k *KYC) updateAddress(ctx *gin.Context) {
	request := struct {
		State           string `json:"state" binding:"required"`
		LGA             string `json:"lga" binding:"required"`
		HouseNumber     string `json:"house_number"`
		StreetName      string `json:"street_name"`
		NearestLandmark string `json:"nearest_landmark" binding:"required"`
	}{}

	err := ctx.ShouldBindJSON(&request)
	if err != nil {
		k.server.logger.Log(logrus.ErrorLevel, err.Error())
		ctx.JSON(http.StatusBadRequest, basemodels.NewError(apistrings.InvalidAddressInput))
		return
	}

	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	/// check varification status
	if !activeUser.Verified {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("you have not verified your account yet"))
		return
	}

	/// Check for User's KYC file or create one if it doesn't exist
	userKyc, err := k.server.queries.GetKYCByUserID(ctx, int32(activeUser.UserID))
	if err == sql.ErrNoRows {
		userKyc, err = k.server.queries.CreateNewKYC(ctx, db.CreateNewKYCParams{
			UserID: int32(activeUser.UserID),
			Tier:   0,
		})
		if err == sql.ErrNoRows {
			ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
			return
		} else if err != nil {
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
			return
		}
	} else if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(apistrings.ServerError))
		return
	}

	args := db.UpdateKYCAddressParams{
		ID: userKyc.ID,
		State: sql.NullString{
			String: request.State,
			Valid:  request.State != "",
		},
		Lga: sql.NullString{
			String: request.LGA,
			Valid:  request.LGA != "",
		},
		HouseNumber: sql.NullString{
			String: request.HouseNumber,
			Valid:  request.HouseNumber != "",
		},
		StreetName: sql.NullString{
			String: request.StreetName,
			Valid:  request.StreetName != "",
		},
		NearestLandmark: sql.NullString{
			String: request.NearestLandmark,
			Valid:  request.NearestLandmark != "",
		},
	}

	tx, err := k.server.queries.DB.Begin()
	if err != nil {
		panic(err)
	}
	defer tx.Rollback()

	kyc, err := k.server.queries.WithTx(tx).UpdateKYCAddress(ctx, args)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("KYC Validation error occurred at the DB Level"))
		return
	}

	err = tx.Commit()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("KYC Validation error occurred at the DB Level"))
		return
	}

	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Update address success", models.ToUserKYCInformation(&kyc)))
}

func (k *KYC) submitUtility(ctx *gin.Context) {
	// Get the form data (file)
	file, header, err := ctx.Request.FormFile("file")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("File is required"))
		return
	}
	defer file.Close()

	// Check file size
	if header.Size > 15*1024*1024 {
		ctx.JSON(http.StatusRequestEntityTooLarge, basemodels.NewError("File size exceeds 15MB"))
		return
	}

	// Read the image data
	imageData, err := io.ReadAll(file)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Error parsing file"))
		return
	}

	// Get the proof type from the form
	proofType := ctx.DefaultPostForm("proof_type", "Utility Bill")

	// Prepare the filename (use the form field or default to "proof_image.png")
	filename := ctx.DefaultPostForm("filename", fmt.Sprintf("utility_image %v.png", time.Now().UTC()))

	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	/// check varification status
	if !activeUser.Verified {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("you have not verified your account yet"))
		return
	}

	// Insert into the database
	proof, err := k.server.queries.InsertNewProofImage(ctx, db.InsertNewProofImageParams{
		UserID:    int32(activeUser.UserID),
		Filename:  filename,
		ProofType: proofType,
		ImageData: imageData,
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to upload proof of address %s", err)))
		return
	}

	// Respond with success
	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Proof of address uploaded successfully!", gin.H{
		"id":         models.ID(proof.ID),
		"user_id":    models.ID(proof.UserID),
		"filename":   proof.Filename,
		"proof_type": proof.ProofType,
		"created_at": proof.CreatedAt,
	}))
}

func (k *KYC) uploadProofOfAddress(ctx *gin.Context) {
	// Get the form data (file)
	file, header, err := ctx.Request.FormFile("file")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("file is required"))
		return
	}
	defer file.Close()

	// Get the proof type from the form
	proofType := ctx.PostForm("proof_type")
	if proofType == "" {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("proof_type is required"))
		return
	}

	allowedProofTypes := []string{"utility_bill", "bank_statement", "tenancy_agreement"}

	// Normalize the input string
	normalizedProofType := strings.ToLower(strings.ReplaceAll(proofType, " ", "_"))

	// Check if proof type is valid
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

	// Check file size
	if header.Size > 15*1024*1024 {
		ctx.JSON(http.StatusRequestEntityTooLarge, basemodels.NewError("File size exceeds 15MB"))
		return
	}

	// Ensure the file type is PNG, JPG, or JPEG
	allowedContentTypes := []string{"image/png", "image/jpeg", "image/jpg"}
	fileContentType := header.Header.Get("Content-Type")
	isValidContentType := false
	for _, allowedType := range allowedContentTypes {
		if fileContentType == allowedType {
			isValidContentType = true
			break
		}
	}

	if !isValidContentType {
		ctx.JSON(http.StatusBadRequest, basemodels.NewError("File type must be PNG, JPG, or JPEG"))
		return
	}

	// Read the image data
	imageData, err := io.ReadAll(file)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError("Error parsing file"))
		return
	}

	// Prepare the filename (use the form field or default to "proof_image.png")
	filename := ctx.DefaultPostForm("filename", fmt.Sprintf("%v %v.png", proofType, time.Now().UTC()))

	// Fetch user details
	activeUser, err := utils.GetActiveUser(ctx)
	if err != nil {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError(apistrings.UserNotFound))
		return
	}

	/// check varification status
	if !activeUser.Verified {
		ctx.JSON(http.StatusUnauthorized, basemodels.NewError("you have not verified your account yet"))
		return
	}

	// Insert into the database
	proof, err := k.server.queries.InsertNewProofImage(ctx, db.InsertNewProofImageParams{
		UserID:    int32(activeUser.UserID),
		Filename:  filename,
		ProofType: proofType,
		ImageData: imageData,
	})
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, basemodels.NewError(fmt.Sprintf("Failed to upload proof of address %s", err)))
		return
	}

	// Respond with success
	ctx.JSON(http.StatusOK, basemodels.NewSuccess("Proof of address uploaded successfully!", gin.H{
		"id":         models.ID(proof.ID),
		"user_id":    models.ID(proof.UserID),
		"filename":   proof.Filename,
		"proof_type": proof.ProofType,
		"created_at": proof.CreatedAt,
		"verified":   proof.Verified,
	}))
}

// Retrieve Proof of Address by Proof ID (GET) - Admin
func (k *KYC) retrieveProofOfAddress(c *gin.Context) {
	// Retrieve the ID from the URL
	id := c.Param("id")
	// Convert the ID to int32
	idObj, err := models.ParseIDFromString(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID"})
		return
	}

	// Fetch the image data from the database
	proof, err := k.server.queries.GetProofImage(c, int32(idObj))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Proof of address not found"})
		return
	}

	// Set the content type based on file extension
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", proof.Filename))

	// Send the image as a response
	c.Data(http.StatusOK, "application/octet-stream", proof.ImageData)
}
