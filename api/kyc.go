package api

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/SwiftFiat/SwiftFiat-Backend/api/apistrings"
	models "github.com/SwiftFiat/SwiftFiat-Backend/api/models"
	db "github.com/SwiftFiat/SwiftFiat-Backend/db/sqlc"
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/service/provider"
	"github.com/SwiftFiat/SwiftFiat-Backend/service/provider/kyc"
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
	serverGroupV1.GET("/", AuthenticatedMiddleware(), k.getUserKyc)
	serverGroupV1.POST("validate-bvn", AuthenticatedMiddleware(), k.validateBVN)
	serverGroupV1.POST("validate-nin", AuthenticatedMiddleware(), k.validateNIN)
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

	if provider, exists := k.server.provider.GetProvider(provider.Dojah); exists {
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

	if provider, exists := k.server.provider.GetProvider(provider.Dojah); exists {
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
